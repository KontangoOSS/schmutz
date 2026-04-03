//go:build linux

package agent

import (
	"bufio"
	"encoding/json"
	"os/exec"
	"time"
)

// LogEntry is a single log line forwarded from journald.
// Short keys keep payload compact before zlib compression.
type LogEntry struct {
	P    int    `json:"p,omitempty"` // syslog priority 0–7
	Unit string `json:"u,omitempty"` // systemd unit
	Msg  string `json:"m"`           // message text
}

// logCollector streams new journal entries as they arrive.
// Uses `journalctl -f -o json` — each line is a JSON object.
type logCollector struct{}

func (c *logCollector) collect(ctx interface{ Done() <-chan struct{} }, machineID string, out chan<- []byte) {
	for {
		tailJournal(ctx, machineID, out)
		select {
		case <-ctx.Done():
			return
		case <-time.After(30 * time.Second):
		}
	}
}

func tailJournal(ctx interface{ Done() <-chan struct{} }, machineID string, out chan<- []byte) {
	cmd := exec.Command("journalctl", "-f", "-o", "json", "--no-pager")
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return
	}
	if err := cmd.Start(); err != nil {
		return
	}
	go func() {
		<-ctx.Done()
		cmd.Process.Kill()
	}()

	sc := bufio.NewScanner(pipe)
	sc.Buffer(make([]byte, 256*1024), 256*1024)
	for sc.Scan() {
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(sc.Bytes(), &raw); err != nil {
			continue
		}

		entry := LogEntry{
			Msg:  jsonStr(raw, "MESSAGE"),
			Unit: jsonStr(raw, "_SYSTEMD_UNIT"),
		}
		if p := jsonStr(raw, "PRIORITY"); len(p) == 1 {
			entry.P = int(p[0] - '0')
		}
		// Only forward priority 0–4 (emerg..warning) to keep traffic low.
		if entry.P > 4 {
			continue
		}

		ts := time.Now().Unix()
		if raw["__REALTIME_TIMESTAMP"] != nil {
			// journald gives microseconds as a quoted string — best effort
			var us string
			if json.Unmarshal(raw["__REALTIME_TIMESTAMP"], &us) == nil && len(us) > 6 {
				ts = parseUsec(us)
			}
		}

		if b, err := encodeEvent(machineID, "log", entry); err == nil {
			_ = ts // ts is embedded by encodeEvent via time.Now; journal ts used for ordering
			select {
			case out <- b:
			default:
			}
		}
	}
	cmd.Wait()
}

func jsonStr(m map[string]json.RawMessage, key string) string {
	v, ok := m[key]
	if !ok {
		return ""
	}
	var s string
	json.Unmarshal(v, &s)
	return s
}

func parseUsec(s string) int64 {
	var n int64
	for _, c := range s {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int64(c-'0')
	}
	return n / 1_000_000 // microseconds → seconds
}
