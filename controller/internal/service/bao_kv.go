package service

import "fmt"

// --- KV v2 extended operations ---

// GetMetadata reads the metadata for a KV v2 secret without the data.
func (s *StoreService) GetMetadata(mount, secretPath string) (map[string]interface{}, error) {
	secret, err := s.client.Logical().Read(mount + "/metadata/" + secretPath)
	if err != nil {
		return nil, err
	}
	if secret == nil {
		return nil, nil
	}
	return secret.Data, nil
}

// PutMetadata updates the metadata for a KV v2 secret.
func (s *StoreService) PutMetadata(mount, secretPath string, data map[string]interface{}) error {
	_, err := s.client.Logical().Write(mount+"/metadata/"+secretPath, data)
	return err
}

// DeleteMetadata permanently removes all version data and metadata.
func (s *StoreService) DeleteMetadata(mount, secretPath string) error {
	_, err := s.client.Logical().Delete(mount + "/metadata/" + secretPath)
	return err
}

// DeleteVersions soft-deletes specific versions of a KV v2 secret.
func (s *StoreService) DeleteVersions(mount, secretPath string, versions []int) error {
	_, err := s.client.Logical().Write(mount+"/delete/"+secretPath, map[string]interface{}{
		"versions": versions,
	})
	return err
}

// UndeleteVersions restores previously soft-deleted versions.
func (s *StoreService) UndeleteVersions(mount, secretPath string, versions []int) error {
	_, err := s.client.Logical().Write(mount+"/undelete/"+secretPath, map[string]interface{}{
		"versions": versions,
	})
	return err
}

// DestroyVersions permanently destroys specific versions (cannot be undone).
func (s *StoreService) DestroyVersions(mount, secretPath string, versions []int) error {
	_, err := s.client.Logical().Write(mount+"/destroy/"+secretPath, map[string]interface{}{
		"versions": versions,
	})
	return err
}

// GetKVConfig reads the configuration of a KV v2 mount.
func (s *StoreService) GetKVConfig(mount string) (map[string]interface{}, error) {
	secret, err := s.client.Logical().Read(mount + "/config")
	if err != nil {
		return nil, err
	}
	if secret == nil {
		return nil, nil
	}
	return secret.Data, nil
}

// PutKVConfig updates the configuration of a KV v2 mount.
func (s *StoreService) PutKVConfig(mount string, maxVersions int, casRequired bool, deleteVersionAfter string) error {
	_, err := s.client.Logical().Write(mount+"/config", map[string]interface{}{
		"max_versions":         maxVersions,
		"cas_required":         casRequired,
		"delete_version_after": deleteVersionAfter,
	})
	return err
}

// GetVersion reads a specific version of a KV v2 secret.
func (s *StoreService) GetSecretVersion(mount, secretPath string, version int) (map[string]interface{}, error) {
	secret, err := s.client.Logical().ReadWithData(mount+"/data/"+secretPath, map[string][]string{
		"version": {fmt.Sprintf("%d", version)},
	})
	if err != nil {
		return nil, err
	}
	if secret == nil {
		return nil, nil
	}
	return secret.Data, nil
}
