package auth

import (
	"fmt"
	"os"
	"path/filepath"
)

// envKeyPathOverride lets ops override the default .p8 location (rarely needed).
const envKeyPathOverride = "APP_STORE_CONNECT_KEY_PATH"

// KeyPath resolves the canonical .p8 location for a given key ID:
//
//	$APP_STORE_CONNECT_KEY_PATH (if set)
//	~/.appstoreconnect/AuthKey_<keyID>.p8 (default)
//
// Returns ErrEmptyKeyID if keyID is empty and no override is set.
func KeyPath(keyID string) (string, error) {
	if override := os.Getenv(envKeyPathOverride); override != "" {
		return override, nil
	}
	if keyID == "" {
		return "", ErrEmptyKeyID
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("auth: resolve home dir: %w", err)
	}
	return filepath.Join(home, ".appstoreconnect", "AuthKey_"+keyID+".p8"), nil
}
