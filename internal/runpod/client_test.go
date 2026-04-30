package runpod

import "testing"

func TestEscapeGQL(t *testing.T) {
	cases := map[string]string{
		"plain":          "plain",
		`with "quotes"`:  `with \"quotes\"`,
		`back\slash`:     `back\\slash`,
		"":               "",
		`mixed\"both`:    `mixed\\\"both`,
	}
	for in, want := range cases {
		if got := escapeGQL(in); got != want {
			t.Errorf("escapeGQL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("short", 10); got != "short" {
		t.Errorf("truncate(short, 10) = %q, want %q", got, "short")
	}
	if got := truncate("0123456789abc", 10); got != "0123456789…" {
		t.Errorf("truncate did not append ellipsis: %q", got)
	}
}

func TestNewClient_DefaultsUserAgent(t *testing.T) {
	c := NewClient("k", "")
	if c.userAgent == "" {
		t.Error("expected NewClient to default the user agent when empty")
	}
}
