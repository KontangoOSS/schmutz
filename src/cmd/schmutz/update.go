package main

import (
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/KontangoOSS/schmutz/root"
	"github.com/spf13/cobra"
)

// updateCmd downloads the latest schmutz binary from the controller's install
// endpoint, verifies its checksum, and atomically swaps it into place.
//
// Today this hits the public 443 install endpoint (with the install code).
// Future (Slice 6 — overlay-only): this should dial schmutz.tango over the
// Ziti overlay so updates don't require a public path. That work is gated on
// having the overlay-served install endpoint.
func updateCmd() *cobra.Command {
	var dryRun bool
	var skipChecksum bool
	var insecure bool
	var noRestart bool
	var withZiti bool
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Self-update the schmutz binary in place",
		Long: "Downloads /install/schmutz-linux-<arch> from the enrolled controller, " +
			"verifies its sha256 against /install/schmutz-SHA256SUMS, and atomically " +
			"replaces the running binary. By default restarts the schmutz systemd unit " +
			"if it exists.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !isRoot() {
				return errors.New("update requires root")
			}
			r, err := root.LoadRoot(schmutzDir)
			if err != nil {
				return fmt.Errorf("load root: %w", err)
			}
			ctrl := strings.TrimRight(r.ControllerURL(), "/")
			if ctrl == "" {
				return errors.New("no controller_url stored — run `schmutz enroll --controller https://...` first")
			}

			arch := runtime.GOARCH
			osName := runtime.GOOS
			binFile := fmt.Sprintf("schmutz-%s-%s", osName, arch)

			selfPath, err := os.Executable()
			if err != nil {
				return fmt.Errorf("locate self: %w", err)
			}

			currentVersion := Version
			fmt.Printf("schmutz: current version %s at %s\n", currentVersion, selfPath)

			code := os.Getenv("SCHMUTZ_INSTALL_CODE")

			client := &http.Client{
				Timeout: 5 * time.Minute,
				Transport: &http.Transport{
					TLSClientConfig: &tls.Config{InsecureSkipVerify: insecure},
				},
			}

			// 1. Fetch checksums first — small file, fail fast on auth issues.
			sumsURL := ctrl + "/install/schmutz-SHA256SUMS"
			fmt.Printf("schmutz: fetching %s\n", sumsURL)
			sumsBytes, err := httpGetWithCode(client, sumsURL, code)
			if err != nil {
				return fmt.Errorf("fetch checksums: %w", err)
			}
			expected := lookupChecksum(string(sumsBytes), binFile)
			if expected == "" {
				return fmt.Errorf("checksum for %s not present in %s", binFile, sumsURL)
			}
			fmt.Printf("schmutz: expected sha256 %s\n", expected)

			// 2. Download new binary to a temp file in same dir as target — keeps
			// the rename within the same filesystem so it's atomic.
			binURL := fmt.Sprintf("%s/install/%s", ctrl, binFile)
			fmt.Printf("schmutz: fetching %s\n", binURL)
			tmpFile, err := os.CreateTemp(filepath.Dir(selfPath), ".schmutz-update-*")
			if err != nil {
				return fmt.Errorf("create temp: %w", err)
			}
			tmpPath := tmpFile.Name()
			defer os.Remove(tmpPath) // best-effort cleanup if we bail

			req, err := http.NewRequest(http.MethodGet, binURL, nil)
			if err != nil {
				tmpFile.Close()
				return fmt.Errorf("build request: %w", err)
			}
			if code != "" {
				req.Header.Set("X-Install-Code", code)
			}
			resp, err := client.Do(req)
			if err != nil {
				tmpFile.Close()
				return fmt.Errorf("download: %w", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != 200 {
				tmpFile.Close()
				return fmt.Errorf("download HTTP %d (need install code? set SCHMUTZ_INSTALL_CODE)", resp.StatusCode)
			}

			h := sha256.New()
			if _, err := io.Copy(io.MultiWriter(tmpFile, h), resp.Body); err != nil {
				tmpFile.Close()
				return fmt.Errorf("write download: %w", err)
			}
			if err := tmpFile.Close(); err != nil {
				return fmt.Errorf("close temp: %w", err)
			}
			actual := hex.EncodeToString(h.Sum(nil))
			fmt.Printf("schmutz: downloaded sha256 %s\n", actual)

			if !skipChecksum && actual != expected {
				return fmt.Errorf("checksum mismatch (expected %s got %s) — refusing to install", expected, actual)
			}

			if dryRun {
				fmt.Println("schmutz: --dry-run set, not replacing binary")
				return nil
			}

			if err := os.Chmod(tmpPath, 0755); err != nil {
				return fmt.Errorf("chmod: %w", err)
			}

			// 3. Atomic swap. Linux allows overwriting a running binary because
			// the kernel keeps the old inode alive for the running process.
			if err := os.Rename(tmpPath, selfPath); err != nil {
				return fmt.Errorf("rename %s → %s: %w", tmpPath, selfPath, err)
			}
			fmt.Printf("schmutz: replaced %s\n", selfPath)

			// 4. Optionally update bundled ziti too. Different file, same flow.
			if withZiti {
				if err := updateZitiBinary(client, ctrl, code); err != nil {
					fmt.Printf("schmutz: warn: ziti update failed: %v\n", err)
				}
			}

			// 5. Restart the service if installed.
			if !noRestart && fileExists(systemdUnitPath) {
				if err := runSystemctl("restart", "schmutz.service"); err != nil {
					return fmt.Errorf("restart: %w", err)
				}
				fmt.Println("schmutz: schmutz.service restarted")
			} else if noRestart {
				fmt.Println("schmutz: --no-restart set, leaving service as-is")
			}

			fmt.Println("schmutz: update complete")
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "download and verify but do not replace the binary")
	cmd.Flags().BoolVar(&skipChecksum, "skip-checksum", false, "DANGEROUS: install without verifying sha256")
	cmd.Flags().BoolVar(&insecure, "insecure", false, "skip TLS verification (for dev controllers)")
	cmd.Flags().BoolVar(&noRestart, "no-restart", false, "do not restart schmutz.service after update")
	cmd.Flags().BoolVar(&withZiti, "with-ziti", false, "also update the bundled ziti binary")
	return cmd
}

func httpGetWithCode(c *http.Client, url, code string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if code != "" {
		req.Header.Set("X-Install-Code", code)
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1 MiB cap on the sums file
}

// lookupChecksum parses a SHA256SUMS file: lines like
//
//	13c00f65a5...  schmutz-linux-amd64
//
// and returns the hex digest matching the requested filename.
func lookupChecksum(body, filename string) string {
	for _, line := range strings.Split(body, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		// Some sha256sum outputs use "*filename" (binary mode prefix). Strip it.
		name := strings.TrimPrefix(fields[1], "*")
		// Filename may include a leading directory ("/tmp/..." or "./schmutz-...").
		if filepath.Base(name) == filename {
			return fields[0]
		}
	}
	return ""
}

// updateZitiBinary fetches /install/ziti-<os>-<arch> if available. The upstream
// ziti releases use different naming, so we look for both ziti-linux-amd64 and
// fall back to a tarball if the controller exposes that style. For now, simple
// flat-file fetch.
func updateZitiBinary(client *http.Client, ctrl, code string) error {
	zitiBin, err := findZitiBinary()
	if err != nil {
		return fmt.Errorf("locate ziti: %w", err)
	}
	url := fmt.Sprintf("%s/install/ziti-%s-%s", ctrl, runtime.GOOS, runtime.GOARCH)
	tmp, err := os.CreateTemp(filepath.Dir(zitiBin), ".ziti-update-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())

	req, _ := http.NewRequest(http.MethodGet, url, nil)
	if code != "" {
		req.Header.Set("X-Install-Code", code)
	}
	resp, err := client.Do(req)
	if err != nil {
		tmp.Close()
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		tmp.Close()
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if _, err := io.Copy(tmp, resp.Body); err != nil {
		tmp.Close()
		return err
	}
	tmp.Close()
	if err := os.Chmod(tmp.Name(), 0755); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), zitiBin); err != nil {
		return err
	}
	fmt.Printf("schmutz: replaced %s\n", zitiBin)
	return nil
}
