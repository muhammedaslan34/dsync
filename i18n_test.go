package main

import (
	"strings"
	"testing"
)

func TestMessagesComplete(t *testing.T) {
	for lang, m := range messages {
		for key := range messages["en"] {
			if strings.HasPrefix(key, "online.") {
				continue // plural forms differ by language
			}
			if m[key] == "" {
				t.Errorf("%s: missing %s", lang, key)
			}
		}
		if m["online.other"] == "" || m["online.zero"] == "" {
			t.Errorf("%s: missing plural forms", lang)
		}
	}
}

func TestMatchLanguage(t *testing.T) {
	for in, want := range map[string]string{
		"fr_FR.UTF-8": "fr", "ar-SA": "ar", "tr": "tr", "en_GB": "en",
		"de_DE.UTF-8": "en", "": "en", "C": "en",
	} {
		if got := matchLanguage(in); got != want {
			t.Errorf("matchLanguage(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDevicesOnline(t *testing.T) {
	for _, c := range []struct {
		lang string
		n    int
		want string
	}{
		{"en", 0, "No paired devices online"},
		{"en", 1, "1 device online"},
		{"en", 3, "3 devices online"},
		{"fr", 1, "1 appareil en ligne"},
		{"tr", 4, "4 cihaz çevrimiçi"},
		{"ar", 1, "جهاز واحد متصل"},
		{"ar", 2, "جهازان متصلان"},
		{"ar", 5, "5 أجهزة متصلة"},
		{"ar", 12, "12 جهازًا متصلًا"},
		{"ar", 100, "100 جهاز متصل"},
	} {
		if got := devicesOnline(c.lang, c.n); got != c.want {
			t.Errorf("devicesOnline(%s, %d) = %q, want %q", c.lang, c.n, got, c.want)
		}
	}
}
