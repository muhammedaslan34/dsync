package client

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"dsync/internal/proto"
)

// ErrFileChanged means the source file changed size while it was being sent.
var ErrFileChanged = errors.New("file changed while sending")

// FileHeader describes a file being sent.
type FileHeader struct {
	FromID     string
	FromName   string
	FromPort   int
	Name       string
	Size       int64
	TransferID string // see TransferID

	// Set for files sent as part of a folder.
	FolderID    string
	FolderRun   string
	FolderSize  int64
	FolderFiles int
	RelPath     string // "/" separated, starting with the folder name
}

// ErrAlreadyReceived means the device already has this file from an earlier
// attempt at sending the same folder, so nothing was sent.
var ErrAlreadyReceived = errors.New("already received")

// TransferID identifies sending one version of one file to one device. It is
// the same every time the same unchanged file is sent, which is what lets an
// interrupted transfer resume.
func TransferID(senderID, path string, size int64, modTime time.Time) string {
	sum := sha256.Sum256(fmt.Appendf(nil, "%s\x00%s\x00%d\x00%d", senderID, path, size, modTime.UnixNano()))
	return hex.EncodeToString(sum[:16])
}

// SendFile streams f to the paired device with key fp. If the device already
// has the start of this transfer, only the rest is sent; the checksum still
// covers the whole file. progress, if set, is called with the number of bytes
// the device has so far. There is no overall timeout, since big files can take
// a long time; cancel ctx to stop.
func (c *Client) SendFile(ctx context.Context, addr, fp string, h FileHeader, f io.ReadSeeker, progress func(int64)) error {
	if fp == "" {
		return errors.New("device is not paired")
	}
	var off proto.OffsetResponse
	offCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	offReq := proto.OffsetRequest{
		TransferID: h.TransferID, Size: h.Size, Name: h.Name,
		FolderID: h.FolderID, FolderRun: h.FolderRun, RelPath: h.RelPath,
		FolderSize: h.FolderSize, FolderFiles: h.FolderFiles,
	}
	err := c.postJSON(offCtx, addr, fp, "/api/v1/file/offset", offReq, &off, http.StatusOK)
	cancel()
	if err != nil {
		return err
	}
	if off.Complete {
		return ErrAlreadyReceived
	}
	if off.Offset < 0 || off.Offset > h.Size {
		off.Offset = 0
	}

	// Hash the part the device already has from the local copy, which is
	// much faster than sending it again.
	hasher := sha256.New()
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if _, err := io.CopyN(hasher, f, off.Offset); err != nil {
		return ErrFileChanged
	}

	hr := &hashingReader{r: f, h: hasher, n: off.Offset, progress: progress}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint(addr, "/api/v1/file"), hr)
	if err != nil {
		return err
	}
	req.ContentLength = -1 // chunked, so the hash can follow as a trailer
	req.Trailer = http.Header{proto.TrailerSHA256: nil}
	hr.onEOF = func() { req.Trailer.Set(proto.TrailerSHA256, hex.EncodeToString(hr.h.Sum(nil))) }
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set(proto.HeaderFromID, h.FromID)
	req.Header.Set(proto.HeaderFromName, url.QueryEscape(h.FromName))
	req.Header.Set(proto.HeaderFromPort, strconv.Itoa(h.FromPort))
	req.Header.Set(proto.HeaderFileName, url.QueryEscape(h.Name))
	req.Header.Set(proto.HeaderFileSize, strconv.FormatInt(h.Size, 10))
	req.Header.Set(proto.HeaderTransferID, h.TransferID)
	req.Header.Set(proto.HeaderOffset, strconv.FormatInt(off.Offset, 10))
	if h.FolderID != "" {
		req.Header.Set(proto.HeaderFolderID, h.FolderID)
		req.Header.Set(proto.HeaderFolderRun, h.FolderRun)
		req.Header.Set(proto.HeaderFolderSize, strconv.FormatInt(h.FolderSize, 10))
		req.Header.Set(proto.HeaderFolderFiles, strconv.Itoa(h.FolderFiles))
		req.Header.Set(proto.HeaderRelPath, url.QueryEscape(h.RelPath))
	}

	resp, err := c.forKey(fp).Do(req)
	if err != nil {
		return unwrap(err)
	}
	defer resp.Body.Close()
	if err := checkStatus(resp, http.StatusNoContent); err != nil {
		return err
	}
	if hr.n != h.Size {
		return ErrFileChanged
	}
	return nil
}

// FolderID identifies sending one folder from one device, so sending it
// again continues where an interrupted attempt stopped.
func FolderID(senderID, root string) string {
	sum := sha256.Sum256([]byte("folder\x00" + senderID + "\x00" + root))
	return hex.EncodeToString(sum[:16])
}

// FolderEnd tells a device that a folder transfer stopped.
func (c *Client) FolderEnd(ctx context.Context, addr, fp string, e proto.FolderEnd) error {
	ctx, cancel := withDefaultTimeout(ctx, 5*time.Second)
	defer cancel()
	return c.postJSON(ctx, addr, fp, "/api/v1/folder/end", e, nil, http.StatusNoContent)
}

// Retryable reports whether sending again might succeed, e.g. after the
// network comes back. Refusals and local problems are not retryable.
func Retryable(err error) bool {
	var se *StatusError
	switch {
	case err == nil, errors.Is(err, ErrFileChanged), errors.Is(err, ErrIdentityChanged), errors.Is(err, ErrAlreadyReceived),
		errors.Is(err, context.Canceled):
		return false
	case errors.As(err, &se):
		if se.Code == http.StatusInsufficientStorage {
			return false // waiting won't free up disk space
		}
		return se.Code >= 500 || se.Code == http.StatusRequestTimeout || se.Code == http.StatusConflict || se.Code == http.StatusTooManyRequests
	}
	return true // network errors
}

type hashingReader struct {
	r        io.Reader
	h        hash.Hash
	n        int64
	progress func(int64)
	onEOF    func()
}

func (hr *hashingReader) Read(p []byte) (int, error) {
	n, err := hr.r.Read(p)
	if n > 0 {
		hr.h.Write(p[:n])
		hr.n += int64(n)
		if hr.progress != nil {
			hr.progress(hr.n)
		}
	}
	if err == io.EOF && hr.onEOF != nil {
		hr.onEOF()
		hr.onEOF = nil
	}
	return n, err
}
