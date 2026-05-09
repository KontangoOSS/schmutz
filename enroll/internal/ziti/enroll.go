package ziti

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	zitiSDK "github.com/openziti/sdk-golang/ziti"
	zitiEnroll "github.com/openziti/sdk-golang/ziti/enroll"
)

// EnrollJWTToJSON exchanges a Ziti one-time JWT for a fully-enrolled identity
// and returns the identity JSON bytes. The enrollment contact is made to the
// Ziti controller address embedded in the JWT; on a controller node this is
// reachable at localhost:6262 (local Ziti enrollment API).
//
// The agent does NOT need to call EnrollJWT itself — the hub calls this
// server-side so the agent never needs to reach port 6262 or 1280 directly.
// The agent receives the completed identity JSON over the existing :443 ALPN
// channel and writes it to disk.
func EnrollJWTToJSON(jwtStr string) ([]byte, error) {
	if jwtStr == "" {
		return nil, fmt.Errorf("enroll: jwt required")
	}

	claims, jwtToken, err := zitiEnroll.ParseToken(jwtStr)
	if err != nil {
		return nil, fmt.Errorf("enroll: parse jwt: %w", err)
	}

	var keyAlg zitiSDK.KeyAlgVar
	_ = keyAlg.Set("EC")

	flags := zitiEnroll.EnrollmentFlags{
		Token:     claims,
		JwtToken:  jwtToken,
		JwtString: jwtStr,
		KeyAlg:    keyAlg,
	}

	cfg, err := zitiEnroll.Enroll(flags)
	if err != nil {
		return nil, fmt.Errorf("enroll: ziti sdk enroll: %w", err)
	}

	data, err := json.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("enroll: marshal identity: %w", err)
	}
	return data, nil
}

// EnrollJWTToFile is a convenience wrapper that calls EnrollJWTToJSON and
// writes the result atomically to path (mode 0600).
func EnrollJWTToFile(jwtStr, path string) error {
	data, err := EnrollJWTToJSON(jwtStr)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("enroll: mkdir: %w", err)
	}
	// Atomic write via temp file.
	tmp, err := os.CreateTemp(filepath.Dir(path), ".identity.*.json")
	if err != nil {
		return fmt.Errorf("enroll: tempfile: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpPath) }
	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		cleanup()
		return fmt.Errorf("enroll: chmod: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		cleanup()
		return fmt.Errorf("enroll: write: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("enroll: close: %w", err)
	}
	return os.Rename(tmpPath, path)
}
