package service

// --- Transit (Encryption as a Service) ---

func (s *StoreService) TransitCreateKey(mount, name string, keyType string, derived, exportable bool) error {
	data := map[string]interface{}{
		"type":       keyType,
		"derived":    derived,
		"exportable": exportable,
	}
	_, err := s.client.Logical().Write(mount+"/keys/"+name, data)
	return err
}

func (s *StoreService) TransitReadKey(mount, name string) (map[string]interface{}, error) {
	secret, err := s.client.Logical().Read(mount + "/keys/" + name)
	if err != nil {
		return nil, err
	}
	if secret == nil {
		return nil, nil
	}
	return secret.Data, nil
}

func (s *StoreService) TransitListKeys(mount string) ([]string, error) {
	secret, err := s.client.Logical().List(mount + "/keys")
	if err != nil {
		return nil, err
	}
	if secret == nil {
		return nil, nil
	}
	return extractKeys(secret.Data), nil
}

func (s *StoreService) TransitDeleteKey(mount, name string) error {
	// Must first allow deletion
	s.client.Logical().Write(mount+"/keys/"+name+"/config", map[string]interface{}{
		"deletion_allowed": true,
	})
	_, err := s.client.Logical().Delete(mount + "/keys/" + name)
	return err
}

func (s *StoreService) TransitUpdateKeyConfig(mount, name string, config map[string]interface{}) error {
	_, err := s.client.Logical().Write(mount+"/keys/"+name+"/config", config)
	return err
}

func (s *StoreService) TransitEncrypt(mount, keyName, plaintext string, context []byte) (string, error) {
	data := map[string]interface{}{
		"plaintext": plaintext,
	}
	if context != nil {
		data["context"] = context
	}
	secret, err := s.client.Logical().Write(mount+"/encrypt/"+keyName, data)
	if err != nil {
		return "", err
	}
	if secret == nil {
		return "", nil
	}
	if v, ok := secret.Data["ciphertext"].(string); ok {
		return v, nil
	}
	return "", nil
}

func (s *StoreService) TransitDecrypt(mount, keyName, ciphertext string, context []byte) (string, error) {
	data := map[string]interface{}{
		"ciphertext": ciphertext,
	}
	if context != nil {
		data["context"] = context
	}
	secret, err := s.client.Logical().Write(mount+"/decrypt/"+keyName, data)
	if err != nil {
		return "", err
	}
	if secret == nil {
		return "", nil
	}
	if v, ok := secret.Data["plaintext"].(string); ok {
		return v, nil
	}
	return "", nil
}

func (s *StoreService) TransitSign(mount, keyName, input, hashAlgorithm string) (string, error) {
	data := map[string]interface{}{
		"input": input,
	}
	if hashAlgorithm != "" {
		data["hash_algorithm"] = hashAlgorithm
	}
	secret, err := s.client.Logical().Write(mount+"/sign/"+keyName, data)
	if err != nil {
		return "", err
	}
	if secret == nil {
		return "", nil
	}
	if v, ok := secret.Data["signature"].(string); ok {
		return v, nil
	}
	return "", nil
}

func (s *StoreService) TransitVerify(mount, keyName, input, signature, hashAlgorithm string) (bool, error) {
	data := map[string]interface{}{
		"input":     input,
		"signature": signature,
	}
	if hashAlgorithm != "" {
		data["hash_algorithm"] = hashAlgorithm
	}
	secret, err := s.client.Logical().Write(mount+"/verify/"+keyName, data)
	if err != nil {
		return false, err
	}
	if secret == nil {
		return false, nil
	}
	if v, ok := secret.Data["valid"].(bool); ok {
		return v, nil
	}
	return false, nil
}

func (s *StoreService) TransitHMAC(mount, keyName, input, algorithm string) (string, error) {
	data := map[string]interface{}{
		"input": input,
	}
	if algorithm != "" {
		data["algorithm"] = algorithm
	}
	secret, err := s.client.Logical().Write(mount+"/hmac/"+keyName, data)
	if err != nil {
		return "", err
	}
	if secret == nil {
		return "", nil
	}
	if v, ok := secret.Data["hmac"].(string); ok {
		return v, nil
	}
	return "", nil
}

func (s *StoreService) TransitGenerateDataKey(mount, keyName, keyType string) (map[string]interface{}, error) {
	secret, err := s.client.Logical().Write(mount+"/datakey/"+keyType+"/"+keyName, nil)
	if err != nil {
		return nil, err
	}
	if secret == nil {
		return nil, nil
	}
	return secret.Data, nil
}

func (s *StoreService) TransitRewrap(mount, keyName, ciphertext string) (string, error) {
	secret, err := s.client.Logical().Write(mount+"/rewrap/"+keyName, map[string]interface{}{
		"ciphertext": ciphertext,
	})
	if err != nil {
		return "", err
	}
	if secret == nil {
		return "", nil
	}
	if v, ok := secret.Data["ciphertext"].(string); ok {
		return v, nil
	}
	return "", nil
}

func (s *StoreService) TransitRotateKey(mount, name string) error {
	_, err := s.client.Logical().Write(mount+"/keys/"+name+"/rotate", nil)
	return err
}

func (s *StoreService) TransitRandom(mount string, bytes int, format string) (string, error) {
	data := map[string]interface{}{"bytes": bytes}
	if format != "" {
		data["format"] = format
	}
	secret, err := s.client.Logical().Write(mount+"/random", data)
	if err != nil {
		return "", err
	}
	if secret == nil {
		return "", nil
	}
	if v, ok := secret.Data["random_bytes"].(string); ok {
		return v, nil
	}
	return "", nil
}

func (s *StoreService) TransitHash(mount, input, algorithm, format string) (string, error) {
	data := map[string]interface{}{"input": input}
	if algorithm != "" {
		data["algorithm"] = algorithm
	}
	if format != "" {
		data["format"] = format
	}
	secret, err := s.client.Logical().Write(mount+"/hash", data)
	if err != nil {
		return "", err
	}
	if secret == nil {
		return "", nil
	}
	if v, ok := secret.Data["sum"].(string); ok {
		return v, nil
	}
	return "", nil
}
