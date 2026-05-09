package enroll

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"
)

// HandleProgress receives installation progress updates from enrolled devices.
// POST /api/progress/{id}
// Body: {"step":"tunnel-connected","tag":"<enrollment-tag>","hostname":"...","nickname":"..."}
//
// The tag is used for bot detection — device must echo back the same tag
// it received in the enrollment SSE stream.
func (m *Module) HandleProgress(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}

	// Extract device ID from path: /api/progress/{id}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/progress/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		http.Error(w, "device id required", http.StatusBadRequest)
		return
	}
	deviceID := parts[0]

	// Parse progress update
	var update struct {
		Step     string `json:"step"`
		Tag      string `json:"tag"`
		Hostname string `json:"hostname"`
		Nickname string `json:"nickname"`
	}
	if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	// Validate enrollment tag — verify it matches what we issued
	if update.Tag == "" {
		log.Printf("progress: %s → missing tag (no bot detection)", deviceID)
		// Allow it — tag might not be available yet
	} else {
		storedTag, err := m.C.Bao.Get("secret", "devices/"+deviceID+"/enrollment-tag")
		if err == nil && storedTag != nil {
			if tagVal, ok := storedTag["tag"].(string); ok && tagVal != update.Tag {
				log.Printf("progress: %s → invalid tag (possible spoofing)", deviceID)
				http.Error(w, "invalid tag", http.StatusForbidden)
				return
			}
		}
	}

	// Store progress update in Bao for dashboard monitoring
	progressData := map[string]interface{}{
		"step":       update.Step,
		"hostname":   update.Hostname,
		"nickname":   update.Nickname,
		"timestamp":  time.Now().Unix(),
		"last_seen":  time.Now().Format(time.RFC3339),
	}
	m.C.Bao.Put("secret", "devices/"+deviceID+"/progress", progressData)

	// Log progress
	log.Printf("progress: %s [%s] step=%s", deviceID, update.Hostname, update.Step)

	// Respond with 200 OK
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ok",
		"id":     deviceID,
		"step":   update.Step,
	})
}
