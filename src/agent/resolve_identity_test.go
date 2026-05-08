package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveZitiIdentityPath(t *testing.T) {
	const valid = `{"ztAPI":"https://ctrl-1.kontango.net:1280","id":{"cert":"-----BEGIN CERT-----"}}`
	const noZtAPI = `{"id":{"cert":"-----BEGIN CERT-----"}}`
	const malformed = `{not json`

	tests := []struct {
		name        string
		tunnelFiles map[string]string // filename -> content, written under fake /etc/ziti
		wantSuffix  string            // path suffix the result must end with
	}{
		{
			name:        "valid tunnel identity preferred",
			tunnelFiles: map[string]string{"machine-aabbccdd.json": valid},
			wantSuffix:  "machine-aabbccdd.json",
		},
		{
			name: "first valid tunnel identity wins when multiple present",
			tunnelFiles: map[string]string{
				"machine-aaaaaaaa.json": valid,
				"machine-bbbbbbbb.json": valid,
			},
			// Glob returns files in lexical order, so aaaa wins.
			wantSuffix: "machine-aaaaaaaa.json",
		},
		{
			name:        "malformed tunnel JSON falls back",
			tunnelFiles: map[string]string{"machine-aabbccdd.json": malformed},
			wantSuffix:  "/identity.json",
		},
		{
			name:        "tunnel identity without ztAPI falls back",
			tunnelFiles: map[string]string{"machine-aabbccdd.json": noZtAPI},
			wantSuffix:  "/identity.json",
		},
		{
			name:        "no tunnel identity falls back",
			tunnelFiles: nil,
			wantSuffix:  "/identity.json",
		},
		{
			name: "non-machine files in tunnel dir are ignored",
			tunnelFiles: map[string]string{
				"router.json":           valid, // wrong prefix
				"machine-aabbccdd.json": valid,
			},
			wantSuffix: "machine-aabbccdd.json",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tmp := t.TempDir()
			tunnelDir := filepath.Join(tmp, "etc", "ziti")
			if err := os.MkdirAll(tunnelDir, 0755); err != nil {
				t.Fatal(err)
			}
			for name, content := range tc.tunnelFiles {
				if err := os.WriteFile(filepath.Join(tunnelDir, name), []byte(content), 0600); err != nil {
					t.Fatal(err)
				}
			}

			schmutzDir := filepath.Join(tmp, "etc", "schmutz")
			if err := os.MkdirAll(schmutzDir, 0755); err != nil {
				t.Fatal(err)
			}

			// Shim the package-level tunnel dir for this test.
			orig := zitiTunnelDir
			zitiTunnelDir = tunnelDir
			t.Cleanup(func() { zitiTunnelDir = orig })

			got := resolveZitiIdentityPath(schmutzDir)
			if !endsWith(got, tc.wantSuffix) {
				t.Errorf("resolveZitiIdentityPath() = %q, want suffix %q", got, tc.wantSuffix)
			}
		})
	}
}

func TestReadPinnedZitiIdentityPath(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(dir string) string // returns the agent.json path to read
		want    string
	}{
		{
			name: "pinned path returned",
			setup: func(dir string) string {
				p := filepath.Join(dir, "agent.json")
				_ = os.WriteFile(p, []byte(`{"ziti_identity_path":"/etc/ziti/machine-deadbeef.json"}`), 0600)
				return p
			},
			want: "/etc/ziti/machine-deadbeef.json",
		},
		{
			name: "field absent returns empty",
			setup: func(dir string) string {
				p := filepath.Join(dir, "agent.json")
				_ = os.WriteFile(p, []byte(`{"role":"some-role"}`), 0600)
				return p
			},
			want: "",
		},
		{
			name: "missing file returns empty",
			setup: func(dir string) string {
				return filepath.Join(dir, "does-not-exist.json")
			},
			want: "",
		},
		{
			name: "malformed JSON returns empty",
			setup: func(dir string) string {
				p := filepath.Join(dir, "agent.json")
				_ = os.WriteFile(p, []byte(`{not json`), 0600)
				return p
			},
			want: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := tc.setup(t.TempDir())
			if got := readPinnedZitiIdentityPath(path); got != tc.want {
				t.Errorf("readPinnedZitiIdentityPath = %q, want %q", got, tc.want)
			}
		})
	}
}

func endsWith(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}
