package service

import (
	"fmt"
	"path"
	"time"

	vault "github.com/hashicorp/vault/api"
)

// StoreService handles all interactions with Bao/Vault for secrets and machine records.
type StoreService struct {
	client *vault.Client
}

// NewStoreService creates a new store service connected to Bao.
func NewStoreService(addr, token string) (*StoreService, error) {
	config := vault.DefaultConfig()
	config.Address = addr
	config.Timeout = 5 * time.Second

	client, err := vault.NewClient(config)
	if err != nil {
		return nil, fmt.Errorf("bao client: %w", err)
	}
	client.SetToken(token)

	return &StoreService{client: client}, nil
}

// Client returns the underlying Vault API client for direct Bao operations
// (e.g. AppRole secret_id generation, auth method calls).
func (s *StoreService) Client() *vault.Client { return s.client }

// Get reads a KV v2 secret.
func (s *StoreService) Get(mount, secretPath string) (map[string]interface{}, error) {
	if s == nil || s.client == nil {
		return nil, nil
	}
	secret, err := s.client.Logical().Read(path.Join(mount, "data", secretPath))
	if err != nil {
		return nil, err
	}
	if secret == nil || secret.Data == nil {
		return nil, nil
	}
	data, ok := secret.Data["data"].(map[string]interface{})
	if !ok {
		return nil, nil
	}
	return data, nil
}

// Put writes a KV v2 secret.
func (s *StoreService) Put(mount, secretPath string, data map[string]interface{}) error {
	if s == nil || s.client == nil {
		return nil
	}
	_, err := s.client.Logical().Write(
		path.Join(mount, "data", secretPath),
		map[string]interface{}{"data": data},
	)
	return err
}

// Delete permanently removes a KV v2 secret (all versions + metadata).
func (s *StoreService) Delete(mount, secretPath string) error {
	if s == nil || s.client == nil {
		return nil
	}
	// Delete all versions first, then metadata
	s.client.Logical().Delete(path.Join(mount, "data", secretPath))
	_, err := s.client.Logical().Delete(path.Join(mount, "metadata", secretPath))
	return err
}

// List lists keys at a KV v2 path.
func (s *StoreService) List(mount, secretPath string) ([]string, error) {
	if s == nil || s.client == nil {
		return nil, nil
	}
	secret, err := s.client.Logical().List(path.Join(mount, "metadata", secretPath))
	if err != nil {
		return nil, err
	}
	if secret == nil || secret.Data == nil {
		return nil, nil
	}
	keysRaw, ok := secret.Data["keys"].([]interface{})
	if !ok {
		return nil, nil
	}
	keys := make([]string, len(keysRaw))
	for i, k := range keysRaw {
		keys[i], _ = k.(string)
	}
	return keys, nil
}

// --- Machine record convenience methods ---

// LookupByHostname checks if a machine with this hostname exists.
func (s *StoreService) LookupByHostname(hostname string) (id, nickname string, exists bool) {
	if s == nil || s.client == nil {
		return "", "", false
	}
	data, err := s.Get("secret", "identities/machines/by-hostname/"+hostname)
	if err != nil || data == nil {
		return "", "", false
	}
	id, _ = data["id"].(string)
	nickname, _ = data["nickname"].(string)
	return id, nickname, id != ""
}

// PutMachine stores a machine record and its hostname index.
func (s *StoreService) PutMachine(id string, data map[string]interface{}) error {
	if err := s.Put("secret", "identities/machines/"+id, data); err != nil {
		return err
	}
	hostname, _ := data["hostname"].(string)
	nickname, _ := data["nickname"].(string)
	if hostname != "" {
		s.Put("secret", "identities/machines/by-hostname/"+hostname,
			map[string]interface{}{"id": id, "nickname": nickname})
	}
	return nil
}

// PutEnrollmentSnapshot writes a complete immutable enrollment record.
// Path: assets/identity/enrollment/<machineID>
// Written once at enrollment — never mutated. Use a separate path for updates.
func (s *StoreService) PutEnrollmentSnapshot(machineID string, data map[string]interface{}) error {
	return s.Put("assets", "identity/enrollment/"+machineID, data)
}

// GetMachine reads a machine record.
func (s *StoreService) GetMachine(id string) (map[string]interface{}, error) {
	return s.Get("secret", "identities/machines/"+id)
}

// IsBanned checks if an identifier is in the ban list.
func (s *StoreService) IsBanned(identifier string) bool {
	if s == nil || s.client == nil {
		return false
	}
	data, err := s.Get("secret", "bans/"+identifier)
	return err == nil && data != nil
}

// BanDevice adds an identifier to the ban list.
func (s *StoreService) BanDevice(identifier, reason string) error {
	return s.Put("secret", "bans/"+identifier, map[string]interface{}{
		"reason":    reason,
		"banned_at": time.Now().Unix(),
	})
}

// UnbanDevice removes an identifier from the ban list (permanent destroy).
func (s *StoreService) UnbanDevice(identifier string) error {
	return s.Delete("secret", "bans/"+identifier)
}

// --- Entity management (person/group → devices) ---

// PutEntity stores a person/group entity that groups multiple device identities.
func (s *StoreService) PutEntity(id string, data map[string]interface{}) error {
	return s.Put("secret", "identities/entities/"+id, data)
}

// GetEntity reads a person/group entity.
func (s *StoreService) GetEntity(id string) (map[string]interface{}, error) {
	return s.Get("secret", "identities/entities/"+id)
}

// ListEntities lists all entity IDs.
func (s *StoreService) ListEntities() ([]string, error) {
	return s.List("secret", "identities/entities/")
}

// LinkDeviceToEntity associates a device (Ziti identity) with an entity (person).
func (s *StoreService) LinkDeviceToEntity(entityID, deviceID, nickname, deviceType string) error {
	entity, err := s.GetEntity(entityID)
	if err != nil || entity == nil {
		return fmt.Errorf("entity %s not found", entityID)
	}

	// Append to devices list
	devices, _ := entity["devices"].([]interface{})
	devices = append(devices, map[string]interface{}{
		"ziti_id":   deviceID,
		"nickname":  nickname,
		"type":      deviceType,
		"linked_at": time.Now().Unix(),
	})
	entity["devices"] = devices

	return s.PutEntity(entityID, entity)
}

// --- Profile management (known machine → auto-config) ---

// GetProfile reads a machine profile (what a known machine should become).
func (s *StoreService) GetProfile(name string) (map[string]interface{}, error) {
	return s.Get("secret", "identities/profiles/"+name)
}

// PutProfile stores a machine profile.
func (s *StoreService) PutProfile(name string, data map[string]interface{}) error {
	return s.Put("secret", "identities/profiles/"+name, data)
}

// ListProfiles lists all profile names.
func (s *StoreService) ListProfiles() ([]string, error) {
	return s.List("secret", "identities/profiles/")
}

// --- Known machines ---

// GetKnownMachine checks if a hostname matches a known machine record.
func (s *StoreService) GetKnownMachine(hostname string) (map[string]interface{}, error) {
	return s.Get("secret", "identities/known/"+hostname)
}

// --- Audit log ---

// WriteAuditLog appends an enrollment event to the audit log.
func (s *StoreService) WriteAuditLog(machineID, event string, details map[string]interface{}) error {
	entry := map[string]interface{}{
		"machine_id": machineID,
		"event":      event,
		"timestamp":  time.Now().Unix(),
	}
	for k, v := range details {
		entry[k] = v
	}
	// Use timestamp-based key for uniqueness
	key := fmt.Sprintf("audit/%s/%d", machineID, time.Now().UnixNano())
	return s.Put("secret", key, entry)
}

// --- Controller info ---

// GetControllerInfo reads this controller's info from Bao.
func (s *StoreService) GetControllerInfo(nodeName string) (map[string]interface{}, error) {
	return s.Get("secret", "infra/controllers/"+nodeName)
}

// --- Honeypot records (browser enrollment events) ---

// PutHoneypot stores a browser enrollment event keyed by browser fingerprint.
// The fingerprint is the FingerprintJS composite hash from the _zt cookie.
func (s *StoreService) PutHoneypot(fingerprint string, data map[string]interface{}) error {
	return s.Put("secret", "identities/honeypot/"+fingerprint, data)
}

// GetHoneypot retrieves a browser enrollment event by fingerprint.
// Returns nil, nil if not found.
func (s *StoreService) GetHoneypot(fingerprint string) (map[string]interface{}, error) {
	return s.Get("secret", "identities/honeypot/"+fingerprint)
}

// TestStore is an in-memory store for unit tests (no Bao required).
type TestStore struct {
	data map[string]map[string]interface{}
}

// NewTestStore returns an empty in-memory store for use in tests.
func NewTestStore() *TestStore {
	return &TestStore{data: make(map[string]map[string]interface{})}
}

func (s *TestStore) PutHoneypot(fingerprint string, data map[string]interface{}) error {
	s.data["honeypot/"+fingerprint] = data
	return nil
}

func (s *TestStore) GetHoneypot(fingerprint string) (map[string]interface{}, error) {
	v := s.data["honeypot/"+fingerprint]
	return v, nil
}
