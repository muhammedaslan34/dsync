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
	TrailerSHA256  = "X-Dsync-Sha256" // hex
)

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
