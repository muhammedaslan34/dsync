// Package proto defines the wire format shared by all dsync devices.
package proto

const (
	Version         = 2
	DiscoveryPort   = 47100 // UDP
	DefaultHTTPPort = 47101 // TCP
	MaxTextBytes    = 1 << 20
)

const (
	TypeDiscover = "discover"
	TypeAnnounce = "announce"
)

// Device describes a dsync instance.
type Device struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	OS   string `json:"os"`
	Port int    `json:"port"`
}

// Packet is exchanged over UDP during discovery.
type Packet struct {
	Type    string `json:"type"`
	Version int    `json:"version"`
	Device
}

// TextMessage is a piece of text sent to another device.
type TextMessage struct {
	FromID   string `json:"from_id"`
	FromName string `json:"from_name"`
	FromPort int    `json:"from_port,omitempty"`
	Text     string `json:"text"`
}

// Headers for POST /api/v1/file. The body is the raw file content, and the
// SHA-256 of it is sent as a trailer so the sender can hash while streaming.
const (
	HeaderFromID   = "X-Dsync-From-Id"
	HeaderFromName = "X-Dsync-From-Name" // URL query-escaped
	HeaderFromPort = "X-Dsync-From-Port"
	HeaderFileName = "X-Dsync-File-Name" // URL query-escaped
	HeaderFileSize = "X-Dsync-File-Size"
	TrailerSHA256  = "X-Dsync-Sha256" // hex, of the whole file
	// A transfer id stays the same when an interrupted transfer is sent
	// again, so the receiver can keep what it already has.
	HeaderTransferID = "X-Dsync-Transfer-Id"
	// HeaderOffset is where the body starts in the file (0 unless resuming).
	HeaderOffset = "X-Dsync-Offset"

	// Files that are part of a folder carry these too. The folder id is
	// the same every time the same folder is sent, so a retry continues it.
	HeaderFolderID    = "X-Dsync-Folder-Id"
	HeaderFolderRun   = "X-Dsync-Folder-Run"   // new for every attempt at sending the folder
	HeaderFolderSize  = "X-Dsync-Folder-Size"  // total bytes
	HeaderFolderFiles = "X-Dsync-Folder-Files" // total file count
	HeaderRelPath     = "X-Dsync-Rel-Path"     // URL query-escaped, "/" separated, starts with the folder name
)

// OffsetRequest asks how much of a transfer the receiver already has
// (POST /api/v1/file/offset).
type OffsetRequest struct {
	TransferID string `json:"transfer_id"`
	Size       int64  `json:"size"`
	// For a file in a folder, so the receiver can tell whether it already
	// has the whole file from an earlier attempt.
	FolderID  string `json:"folder_id,omitempty"`
	FolderRun string `json:"folder_run,omitempty"`
	RelPath   string `json:"rel_path,omitempty"`
}

type OffsetResponse struct {
	Offset int64 `json:"offset"`
	// Complete means the receiver already has this file; skip it.
	Complete bool `json:"complete,omitempty"`
}

// FolderEnd tells the receiver a folder transfer stopped
// (POST /api/v1/folder/end).
type FolderEnd struct {
	FolderID string `json:"folder_id"`
	Status   string `json:"status"` // "done", "failed" or "canceled"
	Error    string `json:"error,omitempty"`
}

// PairRequest asks a device to trust the sender. The sender's key comes from
// its TLS client certificate.
type PairRequest struct {
	FromID   string `json:"from_id"`
	FromName string `json:"from_name"`
	FromPort int    `json:"from_port"`
	OS       string `json:"os"`
}

// PairResponse is returned when the other side accepts.
type PairResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
