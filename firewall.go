package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf16"
)

// Windows Firewall: the installer allows dsync, but it can still be blocked
// (e.g. installed without the installer, or the network is "Public", where
// the rules don't apply). The app checks for that and can fix it with the
// normal Windows admin prompt.

// FirewallStatus is shown in the settings on Windows.
type FirewallStatus struct {
	Supported      bool     `json:"supported"` // Windows only
	RuleOK         bool     `json:"ruleOk"`    // dsync is allowed through the firewall
	PublicNetworks []string `json:"publicNetworks"`
	Error          string   `json:"error,omitempty"`
}

// Problem reports whether other computers are probably blocked.
func (s FirewallStatus) Problem() bool {
	return s.Supported && s.Error == "" && (!s.RuleOK || len(s.PublicNetworks) > 0)
}

// netProfilesCommand lists the connected networks and their category as
// strings (Windows PowerShell would otherwise give enum numbers).
const netProfilesCommand = `Get-NetConnectionProfile | Select-Object Name,@{n='Category';e={$_.NetworkCategory.ToString()}} | ConvertTo-Json -Compress`

// parseNetProfiles returns the names of networks set to Public from the
// output of netProfilesCommand, which is an object for one network and an
// array for several.
func parseNetProfiles(out []byte) ([]string, error) {
	out = []byte(strings.TrimSpace(string(out)))
	if len(out) == 0 {
		return nil, nil // not connected
	}
	type profile struct {
		Name     string
		Category any
	}
	var list []profile
	if out[0] == '[' {
		if err := json.Unmarshal(out, &list); err != nil {
			return nil, err
		}
	} else {
		var p profile
		if err := json.Unmarshal(out, &p); err != nil {
			return nil, err
		}
		list = []profile{p}
	}
	var public []string
	for _, p := range list {
		// "Public", or 0 if it came through as the enum number.
		if c := fmt.Sprint(p.Category); c == "Public" || c == "0" {
			public = append(public, p.Name)
		}
	}
	return public, nil
}

// psQuote quotes s as a PowerShell single-quoted string.
func psQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// firewallScript replaces dsync's firewall rules with ones allowing the
// given programs on private and domain networks.
func firewallScript(programs ...string) string {
	var b strings.Builder
	b.WriteString("$ErrorActionPreference = 'Stop'\n")
	b.WriteString("netsh advfirewall firewall delete rule name=dsync | Out-Null\n")
	for _, p := range programs {
		fmt.Fprintf(&b, "netsh advfirewall firewall add rule name=dsync dir=in action=allow program=%s profile=private,domain enable=yes\n", psQuote(p))
		b.WriteString("if ($LASTEXITCODE -ne 0) { exit 1 }\n")
	}
	return b.String()
}

// privateNetworkScript sets the connected Public networks to Private, which
// is what a home network should be.
const privateNetworkScript = `$ErrorActionPreference = 'Stop'
Get-NetConnectionProfile | Where-Object NetworkCategory -eq 'Public' | Set-NetConnectionProfile -NetworkCategory Private
`

// utf16le encodes s the way PowerShell's -EncodedCommand expects (before
// base64).
func utf16le(s string) []byte {
	u := utf16.Encode([]rune(s))
	b := make([]byte, len(u)*2)
	for i, v := range u {
		b[2*i], b[2*i+1] = byte(v), byte(v>>8)
	}
	return b
}
