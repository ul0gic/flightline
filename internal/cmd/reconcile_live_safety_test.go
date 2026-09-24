package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func c3bValidEnv(dir string) map[string]string {
	return map[string]string{
		"FLIGHTLINE_LIVE_OPT_IN":          c3bOptIn,
		"FLIGHTLINE_LIVE_BUNDLE_ID":       "com.example.sacrificial",
		"FLIGHTLINE_LIVE_VERSION":         "1.2.3",
		"FLIGHTLINE_LIVE_PLATFORM":        "IOS",
		"FLIGHTLINE_LIVE_ALLOW_BUNDLE_ID": "com.example.sacrificial",
		"FLIGHTLINE_LIVE_ALLOW_VERSION":   "1.2.3",
		"FLIGHTLINE_LIVE_ALLOW_PLATFORM":  "IOS",
		"FLIGHTLINE_LIVE_KEY_ID":          "explicit-key",
		"FLIGHTLINE_LIVE_ISSUER_ID":       "explicit-issuer",
		"FLIGHTLINE_LIVE_KEY_PATH":        filepath.Join(dir, "key.p8"),
		"FLIGHTLINE_LIVE_MANIFEST_PATH":   filepath.Join(dir, "restore.json"),
	}
}

func TestC3B_ExactIntentAndAllowlist(t *testing.T) {
	base := c3bValidEnv(t.TempDir())
	for _, tc := range []struct {
		name, key, value string
	}{
		{"missing opt-in", "FLIGHTLINE_LIVE_OPT_IN", ""},
		{"weak opt-in", "FLIGHTLINE_LIVE_OPT_IN", "true"},
		{"missing target", "FLIGHTLINE_LIVE_BUNDLE_ID", ""},
		{"different bundle", "FLIGHTLINE_LIVE_ALLOW_BUNDLE_ID", "com.example.production"},
		{"different version", "FLIGHTLINE_LIVE_ALLOW_VERSION", "2.0"},
		{"missing platform", "FLIGHTLINE_LIVE_PLATFORM", ""},
		{"different platform", "FLIGHTLINE_LIVE_ALLOW_PLATFORM", "MAC_OS"},
		{"default credential forbidden", "FLIGHTLINE_LIVE_KEY_PATH", ""},
		{"relative manifest", "FLIGHTLINE_LIVE_MANIFEST_PATH", "restore.json"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := make(map[string]string, len(base))
			for k, v := range base {
				env[k] = v
			}
			env[tc.key] = tc.value
			if _, err := c3bValidateTarget(env); err == nil {
				t.Fatal("unsafe live configuration was accepted")
			}
		})
	}
	target, err := c3bValidateTarget(base)
	if err != nil {
		t.Fatalf("explicit exact target rejected: %v", err)
	}
	if target.BundleID != base["FLIGHTLINE_LIVE_BUNDLE_ID"] || target.Version != base["FLIGHTLINE_LIVE_VERSION"] {
		t.Fatalf("wrong target selected: %+v", target)
	}
}

func TestC3B_RestorationManifestPersistsBaseline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "restore.json")
	now := time.Now().UTC()
	m := c3bManifest{SchemaVersion: 1, BundleID: "com.example.sacrificial", Version: "1.2.3", Platform: "IOS", Field: c3bCopyrightPathForTest, Baseline: "Original", Target: "Original [flightline live check]", Status: "baseline-captured", CapturedAt: now, UpdatedAt: now}
	if err := c3bCreateManifest(path, m); err != nil {
		t.Fatal(err)
	}
	if err := c3bCreateManifest(path, m); err == nil {
		t.Fatal("existing restoration manifest was overwritten")
	}
	if err := c3bUpdateManifest(path, c3bManifest{SchemaVersion: 1, BundleID: m.BundleID, Version: m.Version, Platform: m.Platform, Field: m.Field, Baseline: m.Baseline, Target: m.Target, Status: "restoration-unverified", CapturedAt: now}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got c3bManifest
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got.Baseline != "Original" || got.Status != "restoration-unverified" || got.BundleID != m.BundleID {
		t.Fatalf("restoration evidence lost: %+v", got)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("manifest permissions = %o", info.Mode().Perm())
	}
}

const c3bCopyrightPathForTest = "/spec/version/copyright"
