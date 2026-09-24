package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const c3bOptIn = "I_AUTHORIZE_ONE_REVERSIBLE_COPYRIGHT_CHANGE"

type c3bTarget struct {
	BundleID     string
	Version      string
	Platform     string
	KeyID        string
	IssuerID     string
	KeyPath      string
	ManifestPath string
}

func c3bValidateTarget(env map[string]string) (c3bTarget, error) {
	get := func(k string) string { return strings.TrimSpace(env[k]) }
	if get("FLIGHTLINE_LIVE_OPT_IN") != c3bOptIn {
		return c3bTarget{}, errors.New("live reconcile requires exact FLIGHTLINE_LIVE_OPT_IN")
	}
	t := c3bTarget{
		BundleID: get("FLIGHTLINE_LIVE_BUNDLE_ID"), Version: get("FLIGHTLINE_LIVE_VERSION"),
		Platform: get("FLIGHTLINE_LIVE_PLATFORM"), KeyID: get("FLIGHTLINE_LIVE_KEY_ID"),
		IssuerID: get("FLIGHTLINE_LIVE_ISSUER_ID"), KeyPath: get("FLIGHTLINE_LIVE_KEY_PATH"),
		ManifestPath: get("FLIGHTLINE_LIVE_MANIFEST_PATH"),
	}
	if t.BundleID == "" || t.Version == "" || t.Platform != "IOS" || t.KeyID == "" || t.IssuerID == "" || t.KeyPath == "" || t.ManifestPath == "" {
		return c3bTarget{}, errors.New("live reconcile requires explicit bundle ID, version, IOS platform, credential fields, and manifest path")
	}
	if get("FLIGHTLINE_LIVE_ALLOW_BUNDLE_ID") != t.BundleID || get("FLIGHTLINE_LIVE_ALLOW_VERSION") != t.Version || get("FLIGHTLINE_LIVE_ALLOW_PLATFORM") != t.Platform {
		return c3bTarget{}, errors.New("live reconcile target does not match exact app and version allowlist")
	}
	if !filepath.IsAbs(t.KeyPath) || !filepath.IsAbs(t.ManifestPath) || filepath.Clean(t.KeyPath) == filepath.Clean(t.ManifestPath) {
		return c3bTarget{}, errors.New("live reconcile requires distinct absolute key and manifest paths")
	}
	return t, nil
}

type c3bManifest struct {
	SchemaVersion int       `json:"schemaVersion"`
	BundleID      string    `json:"bundleId"`
	Version       string    `json:"version"`
	Platform      string    `json:"platform"`
	Field         string    `json:"field"`
	Baseline      string    `json:"baseline"`
	Target        string    `json:"target"`
	Status        string    `json:"status"`
	CapturedAt    time.Time `json:"capturedAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
	Note          string    `json:"note,omitempty"`
}

func c3bCreateManifest(path string, m c3bManifest) error {
	if !filepath.IsAbs(path) {
		return errors.New("restoration manifest path must be absolute")
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("encode restoration manifest: %w", err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) // #nosec G304 -- explicit absolute manifest path is required by the opt-in gate
	if err != nil {
		return fmt.Errorf("create restoration manifest: %w", err)
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		_ = f.Close()
		return fmt.Errorf("write restoration manifest: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return fmt.Errorf("sync restoration manifest: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close restoration manifest: %w", err)
	}
	return nil
}

func c3bUpdateManifest(path string, m c3bManifest) error {
	m.UpdatedAt = time.Now().UTC()
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("encode restoration manifest: %w", err)
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".flightline-reconcile-*")
	if err != nil {
		return fmt.Errorf("create temporary restoration manifest: %w", err)
	}
	defer func() { _ = os.Remove(f.Name()) }() // Temporary file cleanup is best effort after rename.
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		return fmt.Errorf("protect restoration manifest: %w", err)
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		_ = f.Close()
		return fmt.Errorf("write restoration manifest: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return fmt.Errorf("sync restoration manifest: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close restoration manifest: %w", err)
	}
	if err := os.Rename(f.Name(), path); err != nil {
		return fmt.Errorf("replace restoration manifest: %w", err)
	}
	return nil
}
