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

	"dsync/internal/proto"
)

// FileHeader describes a file being sent.
type FileHeader struct {
	FromID   string
	FromName string
	FromPort int
	Name     string
	Size     int64
}

// SendFile streams r to the paired device with key fp. progress, if set, is
// called with the number of bytes sent so far. There is no overall timeout,
// since big files can take a long time; cancel ctx to stop.
func (c *Client) SendFile(ctx context.Context, addr, fp string, h FileHeader, r io.Reader, progress func(int64)) error {
	if fp == "" {
		return errors.New("device is not paired")
	}
	hr := &hashingReader{r: r, h: sha256.New(), progress: progress}
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

	resp, err := c.forKey(fp).Do(req)
	if err != nil {
		return unwrap(err)
	}
	defer resp.Body.Close()
	if err := checkStatus(resp, http.StatusNoContent); err != nil {
		return err
	}
	if hr.n != h.Size {
		return fmt.Errorf("file changed while sending (%d of %d bytes)", hr.n, h.Size)
	}
	return nil
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
