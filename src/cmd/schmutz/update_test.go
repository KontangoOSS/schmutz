package main

import (
	"strings"
	"testing"
)

func TestLookupChecksum(t *testing.T) {
	body := strings.Join([]string{
		"13c00f65a5ee83f9b954731d03bbe4809bcd7055a52a15f68cdaa25fd7d24f1a  schmutz-linux-amd64",
		"29546232ba36ae8ea3e2671390ed5b87d5d7bcf5754afc70aeb25072a1880a27  schmutz-linux-arm64",
		"deadbeef00000000000000000000000000000000000000000000000000000000  /tmp/schmutz-SHA256SUMS",
	}, "\n")

	cases := []struct {
		name string
		want string
	}{
		{"schmutz-linux-amd64", "13c00f65a5ee83f9b954731d03bbe4809bcd7055a52a15f68cdaa25fd7d24f1a"},
		{"schmutz-linux-arm64", "29546232ba36ae8ea3e2671390ed5b87d5d7bcf5754afc70aeb25072a1880a27"},
		{"schmutz-SHA256SUMS", "deadbeef00000000000000000000000000000000000000000000000000000000"},
		{"schmutz-darwin-arm64", ""}, // not present
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := lookupChecksum(body, c.name)
			if got != c.want {
				t.Errorf("lookupChecksum(%q) = %q, want %q", c.name, got, c.want)
			}
		})
	}
}

func TestLookupChecksumHandlesBinaryModeStar(t *testing.T) {
	// `sha256sum -b` emits "*filename" with a leading asterisk.
	body := "abc123  *schmutz-linux-amd64\n"
	got := lookupChecksum(body, "schmutz-linux-amd64")
	if got != "abc123" {
		t.Errorf("got %q, want abc123", got)
	}
}

func TestLookupChecksumIgnoresBlankLines(t *testing.T) {
	body := "\n\n  \nabc123  schmutz-linux-amd64\n\n"
	got := lookupChecksum(body, "schmutz-linux-amd64")
	if got != "abc123" {
		t.Error("blank-line-tolerant parser failed")
	}
}

func TestLookupChecksumEmpty(t *testing.T) {
	if got := lookupChecksum("", "anything"); got != "" {
		t.Errorf("got %q for empty body", got)
	}
}
