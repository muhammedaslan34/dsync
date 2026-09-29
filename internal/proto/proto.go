// Package proto defines the wire format shared by all dsync devices.
package proto

const (
	Version         = 1
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
