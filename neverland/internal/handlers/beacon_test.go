package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	tinkv1 "github.com/tinkerbell/tink/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"git.konoss.org/kore/schmutz/neverland/internal/beacon"
	"git.konoss.org/kore/schmutz/neverland/internal/handlers"
)

// fakeBeaconStore satisfies handlers.BeaconStore without postgres for unit tests.
type fakeBeaconStore struct {
	rows []beacon.Row
}

func (f *fakeBeaconStore) Insert(_ context.Context, r beacon.Row) error {
	f.rows = append(f.rows, r)
	return nil
}

func TestBeacon_NewMACCreatesHardwareCRAndSession(t *testing.T) {
	fakeClient := fake.NewClientBuilder().WithScheme(testScheme()).Build()
	store := &fakeBeaconStore{}
	cfg := handlers.BeaconConfig{
		BootBaseURL: "https://boot.kontango.net",
		Namespace:   "tink-system",
	}
	h := handlers.NewBeaconHandler(fakeClient, store, cfg)

	body := `{"level":"ipxe","fingerprint":{"mac":"bc:24:11:aa:bb:cc","ip":"192.0.2.10","arch":"amd64"}}`
	req := httptest.NewRequest("POST", "/api/v1/beacon", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.Handle(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["session_id"] == "" {
		t.Fatalf("missing session_id: %+v", resp)
	}
	if resp["short_code"] == "" {
		t.Fatalf("missing short_code: %+v", resp)
	}
	if got, ok := resp["short_code"].(string); !ok || len(got) != 9 {
		t.Fatalf("short_code malformed (expect 9-char XXXX-XXXX): %v", resp["short_code"])
	}
	if resp["skip_claim"] != false {
		t.Fatalf("expected skip_claim=false, got %v", resp["skip_claim"])
	}
	if len(store.rows) != 1 {
		t.Fatalf("expected 1 beacon row, got %d", len(store.rows))
	}

	// Verify a Hardware CR was created
	var hwList tinkv1.HardwareList
	if err := fakeClient.List(req.Context(), &hwList); err != nil {
		t.Fatalf("list hw: %v", err)
	}
	if len(hwList.Items) != 1 {
		t.Fatalf("expected 1 Hardware CR, got %d", len(hwList.Items))
	}
	if hwList.Items[0].Spec.Interfaces[0].DHCP.MAC != "bc:24:11:aa:bb:cc" {
		t.Fatalf("MAC mismatch: %s", hwList.Items[0].Spec.Interfaces[0].DHCP.MAC)
	}
	if hwList.Items[0].Spec.Interfaces[0].Netboot.AllowPXE == nil ||
		*hwList.Items[0].Spec.Interfaces[0].Netboot.AllowPXE {
		t.Fatalf("expected allowPXE=false on auto-discovered hardware")
	}
}

func TestBeacon_KnownClaimedMACReturnsSkipClaim(t *testing.T) {
	hw := &tinkv1.Hardware{}
	hw.Name = "auto-bc24-11dd-eeff"
	hw.Namespace = "tink-system"
	hw.Annotations = map[string]string{"kontango.io/claimed-by": "alice@example.com"}
	allow := false
	hw.Spec.Interfaces = []tinkv1.Interface{{
		DHCP:    &tinkv1.DHCP{MAC: "bc:24:11:dd:ee:ff"},
		Netboot: &tinkv1.Netboot{AllowPXE: &allow},
	}}

	fakeClient := fake.NewClientBuilder().WithScheme(testScheme()).WithObjects(hw).Build()
	h := handlers.NewBeaconHandler(fakeClient, &fakeBeaconStore{}, handlers.BeaconConfig{
		BootBaseURL: "https://boot.kontango.net",
		Namespace:   "tink-system",
	})

	body := `{"level":"ipxe","fingerprint":{"mac":"bc:24:11:dd:ee:ff","ip":"192.0.2.20","arch":"amd64"}}`
	req := httptest.NewRequest("POST", "/api/v1/beacon", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	h.Handle(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["skip_claim"] != true {
		t.Fatalf("expected skip_claim=true for already-claimed MAC, got %v", resp["skip_claim"])
	}
}

func TestBeacon_TinkerbellUserClassReturnsSkipClaim(t *testing.T) {
	fakeClient := fake.NewClientBuilder().WithScheme(testScheme()).Build()
	h := handlers.NewBeaconHandler(fakeClient, &fakeBeaconStore{}, handlers.BeaconConfig{
		BootBaseURL: "https://boot.kontango.net",
		Namespace:   "tink-system",
	})
	body := `{"level":"ipxe","fingerprint":{"mac":"aa:bb:cc:dd:ee:01","userclass":"Tinkerbell","arch":"amd64"}}`
	req := httptest.NewRequest("POST", "/api/v1/beacon", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	h.Handle(w, req)
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["skip_claim"] != true {
		t.Fatalf("expected skip_claim=true for Tinkerbell user class, got %v", resp["skip_claim"])
	}
}

func TestBeacon_IpxeFormat(t *testing.T) {
	fakeClient := fake.NewClientBuilder().WithScheme(testScheme()).Build()
	h := handlers.NewBeaconHandler(fakeClient, &fakeBeaconStore{}, handlers.BeaconConfig{
		BootBaseURL: "https://boot.kontango.net",
		Namespace:   "tink-system",
	})
	body := `{"level":"ipxe","fingerprint":{"mac":"00:11:22:33:44:55","arch":"amd64"}}`
	req := httptest.NewRequest("POST", "/api/v1/beacon?ipxe=1", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	h.Handle(w, req)
	out := w.Body.String()
	if !strings.HasPrefix(out, "#!ipxe\n") {
		t.Fatalf("expected iPXE script, got: %s", out)
	}
	if !strings.Contains(out, "set kontango_session_id ") {
		t.Fatalf("missing session_id set: %s", out)
	}
	if !strings.Contains(out, "set kontango_skip_claim 0") {
		t.Fatalf("missing skip_claim: %s", out)
	}
}

func TestBeacon_GetFromIpxe(t *testing.T) {
	fakeClient := fake.NewClientBuilder().WithScheme(testScheme()).Build()
	h := handlers.NewBeaconHandler(fakeClient, &fakeBeaconStore{}, handlers.BeaconConfig{
		BootBaseURL: "https://boot.kontango.net",
		Namespace:   "tink-system",
	})
	// iPXE-style GET: query params, no body
	req := httptest.NewRequest("GET",
		"/api/v1/beacon?ipxe=1&mac=aa:bb:cc:dd:ee:00&ip=192.0.2.5&arch=amd64&platform=pcbios&userclass=iPXE",
		nil)
	w := httptest.NewRecorder()
	h.Handle(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	out := w.Body.String()
	if !strings.HasPrefix(out, "#!ipxe") {
		t.Fatalf("expected iPXE script, got: %s", out)
	}
}

func TestBeacon_GetWithoutMACFails(t *testing.T) {
	fakeClient := fake.NewClientBuilder().WithScheme(testScheme()).Build()
	h := handlers.NewBeaconHandler(fakeClient, &fakeBeaconStore{}, handlers.BeaconConfig{
		BootBaseURL: "https://boot.kontango.net",
		Namespace:   "tink-system",
	})
	req := httptest.NewRequest("GET", "/api/v1/beacon?ipxe=1&arch=amd64", nil)
	w := httptest.NewRecorder()
	h.Handle(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestBeacon_RepeatedMACReturnsSameSession(t *testing.T) {
	fakeClient := fake.NewClientBuilder().WithScheme(testScheme()).Build()
	h := handlers.NewBeaconHandler(fakeClient, &fakeBeaconStore{}, handlers.BeaconConfig{
		BootBaseURL: "https://boot.kontango.net",
		Namespace:   "tink-system",
	})

	body := `{"level":"ipxe","fingerprint":{"mac":"bc:24:11:01:02:03","arch":"amd64"}}`
	mkReq := func() *http.Request {
		return httptest.NewRequest("POST", "/api/v1/beacon", bytes.NewBufferString(body))
	}

	w1 := httptest.NewRecorder()
	h.Handle(w1, mkReq())
	w2 := httptest.NewRecorder()
	h.Handle(w2, mkReq())

	var r1, r2 map[string]interface{}
	json.Unmarshal(w1.Body.Bytes(), &r1)
	json.Unmarshal(w2.Body.Bytes(), &r2)
	if r1["session_id"] != r2["session_id"] {
		t.Fatalf("session_id changed across calls: %v vs %v", r1["session_id"], r2["session_id"])
	}
	if r1["short_code"] != r2["short_code"] {
		t.Fatalf("short_code changed: %v vs %v", r1["short_code"], r2["short_code"])
	}
}
