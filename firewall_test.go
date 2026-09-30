package main

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseNetProfiles(t *testing.T) {
	for _, c := range []struct {
		out  string
		want []string
	}{
		{``, nil},
		{`{"Name":"HomeWifi","Category":"Private"}`, nil},
		{`{"Name":"HomeWifi","Category":"Public"}`, []string{"HomeWifi"}},
		{`[{"Name":"Ethernet","Category":"DomainAuthenticated"},{"Name":"Cafe","Category":"Public"}]`, []string{"Cafe"}},
		{`{"Name":"Old","Category":0}`, []string{"Old"}}, // enum as a number
		{`{"Name":"Old","Category":1}`, nil},
	} {
		got, err := parseNetProfiles([]byte(c.out))
		if err != nil || !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %v, %v; want %v", c.out, got, err, c.want)
		}
	}
	if _, err := parseNetProfiles([]byte("not json")); err == nil {
		t.Error("bad output should be an error")
	}
}

func TestFirewallScriptQuoting(t *testing.T) {
	s := firewallScript(`C:\Program Files\dsync\dsync-gui.exe`, `C:\Users\O'Brien\dsync.exe`)
	for _, want := range []string{
		`program='C:\Program Files\dsync\dsync-gui.exe' profile=private,domain`,
		`program='C:\Users\O''Brien\dsync.exe'`,
		"netsh advfirewall firewall delete rule name=dsync",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("script lacks %q:\n%s", want, s)
		}
	}
}

func TestUTF16LE(t *testing.T) {
	if got := utf16le("aé"); !reflect.DeepEqual(got, []byte{'a', 0, 0xe9, 0}) {
		t.Errorf("got %v", got)
	}
}
