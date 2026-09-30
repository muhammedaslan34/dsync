package control

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// Display is one of a host's screens, as Sunshine sees it.
type Display struct {
	ID      string `json:"id"`   // Sunshine's output_name for it
	Name    string `json:"name"` // e.g. "DELL U2720Q"
	Primary bool   `json:"primary"`
	Width   int    `json:"width"`
	Height  int    `json:"height"`
}

// displayListMarker starts the list of screens Sunshine logs on Windows at
// startup, followed by a JSON array (libdisplaydevice's EnumeratedDevice).
const displayListMarker = "Currently available display devices:"

// parseDisplayLog returns the screens from the last list in a Sunshine log.
// Screens that are switched off (no "info") are left out.
func parseDisplayLog(log []byte) []Display {
	i := bytes.LastIndex(log, []byte(displayListMarker))
	if i < 0 {
		return nil
	}
	rest := log[i+len(displayListMarker):]
	start := bytes.IndexByte(rest, '[')
	if start < 0 {
		return nil
	}
	var devices []struct {
		DeviceID     string `json:"device_id"`
		DisplayName  string `json:"display_name"`
		FriendlyName string `json:"friendly_name"`
		Info         *struct {
			Resolution struct {
				Width  int `json:"width"`
				Height int `json:"height"`
			} `json:"resolution"`
			Primary bool `json:"primary"`
		} `json:"info"`
	}
	// The decoder stops after the array, before the next log lines.
	if err := json.NewDecoder(bytes.NewReader(rest[start:])).Decode(&devices); err != nil {
		return nil
	}
	var out []Display
	for _, d := range devices {
		if d.Info == nil || d.DeviceID == "" {
			continue
		}
		name := d.FriendlyName
		if name == "" {
			name = d.DisplayName
		}
		out = append(out, Display{ID: d.DeviceID, Name: name, Primary: d.Info.Primary,
			Width: d.Info.Resolution.Width, Height: d.Info.Resolution.Height})
	}
	// Main screen first.
	sort.SliceStable(out, func(i, j int) bool { return out[i].Primary && !out[j].Primary })
	return out
}

// sunshineLogPath is where Sunshine writes its log: its log_path setting,
// or the default for this OS.
func sunshineLogPath(cfg map[string]any) string {
	var dir string
	switch runtimeOS {
	case "windows":
		dir = filepath.Join(os.Getenv("ProgramFiles"), "Sunshine", "config")
	default:
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".config", "sunshine")
	}
	if p, ok := cfg["log_path"].(string); ok && p != "" {
		if filepath.IsAbs(p) {
			return p
		}
		return filepath.Join(dir, p)
	}
	return filepath.Join(dir, "sunshine.log")
}

// Displays lists the host's screens from Sunshine's log. Only Sunshine on
// Windows logs them; elsewhere the list is empty.
func Displays(cfg map[string]any) ([]Display, error) {
	b, err := os.ReadFile(sunshineLogPath(cfg))
	if err != nil {
		return nil, fmt.Errorf("could not read Sunshine's log: %w", err)
	}
	return parseDisplayLog(b), nil
}
