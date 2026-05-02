package cmd

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/viper"
	"github.com/ul0gic/skipper/internal/asc"
	"github.com/ul0gic/skipper/internal/auth"
)

// errMissingCreds is the canonical "creds aren't configured" error. The
// message names every input the user can fix, in priority order.
var errMissingCreds = errors.New(
	"missing App Store Connect credentials — set APP_STORE_CONNECT_KEY_ID, " +
		"APP_STORE_CONNECT_ISSUER_ID, or pass --key-id/--issuer-id",
)

// newClient builds a Client from the viper-merged config (flags > env > file).
//
// Returns errMissingCreds if either id is empty so cobra prints a useful hint
// rather than a path-not-found error from auth.KeyPath.
func newClient() (*asc.Client, error) {
	keyID := strings.TrimSpace(viper.GetString("key_id"))
	issuerID := strings.TrimSpace(viper.GetString("issuer_id"))
	if keyID == "" || issuerID == "" {
		return nil, errMissingCreds
	}
	keyPath, err := auth.KeyPath(keyID)
	if err != nil {
		return nil, fmt.Errorf("resolve key path: %w", err)
	}
	c, err := asc.New(asc.Options{
		KeyID:    keyID,
		IssuerID: issuerID,
		KeyPath:  keyPath,
	})
	if err != nil {
		return nil, fmt.Errorf("init ASC client: %w", err)
	}
	return c, nil
}

// outputMode reads the --output flag (default "table") with viper precedence.
func outputMode() string {
	mode := strings.TrimSpace(viper.GetString("output"))
	if mode == "" {
		return "table"
	}
	return mode
}
