package config

import (
	"os"
	"path/filepath"
	"testing"

	yaml "go.yaml.in/yaml/v3"
)

func TestG6_PreviewIdentitySurvivesSnapshotReload(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "local.mov"), []byte("local bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"observed.mov", "local.mov"} {
		t.Run(name, func(t *testing.T) {
			observed := "0123456789abcdef0123456789abcdef"
			state := State{APIVersion: "flightline.dev/v1alpha1", Kind: "AppState", Metadata: StateMetadata{BundleID: "com.example.app", Version: "1.0", Platform: "IOS"}, Spec: StateSpec{
				Previews: &PreviewsSpec{Locales: map[string]map[string][]PreviewFile{"en-US": {"IPHONE_67": {{Path: name, SourceFileChecksum: observed}}}}},
				AppEULA:  &AppEULASpec{AgreementText: new(""), Territories: &[]string{}},
			}}
			data, err := yaml.Marshal(state)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "state.yaml")
			if err := os.WriteFile(path, data, 0o600); err != nil {
				t.Fatal(err)
			}
			loaded, err := LoadState(path)
			if err != nil {
				t.Fatal(err)
			}
			if diagnostics := Validate(path, loaded); len(diagnostics) != 0 {
				t.Fatalf("snapshot validation=%+v", diagnostics)
			}
			want := observed
			if name == "local.mov" {
				want = assetChecksum(dir, name)
			}
			if got := loaded.Spec.Previews.Locales["en-US"]["IPHONE_67"][0].SourceFileChecksum; got != want {
				t.Fatalf("checksum=%s want=%s", got, want)
			}
		})
	}
}
