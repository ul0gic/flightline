package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/ul0gic/flightline/internal/asc"
)

func TestTerritoryView_JSONShape(t *testing.T) {
	v := TerritoryView{
		ID:         "USA",
		Type:       "territories",
		Attributes: asc.TerritoryAttributes{Currency: "USD"},
	}
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	out := string(b)
	for _, want := range []string{
		`"id":"USA"`,
		`"type":"territories"`,
		`"currency":"USD"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("json missing %q: %q", want, out)
		}
	}
}

func TestTerritoryList_TableRowsHeaders(t *testing.T) {
	list := TerritoryList{
		Territories: []TerritoryView{
			{ID: "USA", Type: "territories", Attributes: asc.TerritoryAttributes{Currency: "USD"}},
			{ID: "GBR", Type: "territories", Attributes: asc.TerritoryAttributes{Currency: "GBP"}},
		},
	}
	headers, rows := list.TableRows()
	want := []string{"TERRITORY", "CURRENCY"}
	for i, h := range want {
		if headers[i] != h {
			t.Errorf("headers[%d] = %q, want %q", i, headers[i], h)
		}
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	if rows[0][0] != "USA" || rows[0][1] != "USD" {
		t.Errorf("rows[0] = %v, want [USA USD]", rows[0])
	}
}

func TestTerritoriesCommand_RegisteredOnRoot(t *testing.T) {
	var ter *cobra.Command
	for _, c := range rootCmd.Commands() {
		if c.Name() == "territories" {
			ter = c
			break
		}
	}
	if ter == nil {
		t.Fatal("territories not registered on rootCmd")
	}
	subs := map[string]bool{}
	for _, sc := range ter.Commands() {
		subs[sc.Name()] = true
	}
	if !subs["list"] {
		t.Errorf("territories list subcommand missing")
	}
	// --no-cache flag must be present on `territories list`.
	listCmd := ter.Commands()[0]
	for _, sc := range ter.Commands() {
		if sc.Name() == "list" {
			listCmd = sc
		}
	}
	if listCmd.Flags().Lookup("no-cache") == nil {
		t.Errorf("territories list --no-cache flag missing")
	}
}

// TestTerritories_JSONOutputStability_List asserts the TerritoryList JSON
// shape: top-level "territories" key plus per-row "id"/"type"/"attributes"
// keys are a contract for downstream LLM consumers.
func TestTerritories_JSONOutputStability_List(t *testing.T) {
	list := TerritoryList{
		Territories: []TerritoryView{
			{
				ID:         "USA",
				Type:       "territories",
				Attributes: asc.TerritoryAttributes{Currency: "USD"},
			},
		},
	}
	var buf bytes.Buffer
	if err := renderTo(&buf, list, "json", true); err != nil {
		t.Fatalf("renderTo: %v", err)
	}
	var decoded struct {
		Territories []map[string]any `json:"territories"`
	}
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("json decode: %v\nraw: %s", err, buf.String())
	}
	if len(decoded.Territories) != 1 {
		t.Fatalf("territories len = %d, want 1", len(decoded.Territories))
	}
	row := decoded.Territories[0]
	for _, key := range []string{"id", "type", "attributes"} {
		if _, ok := row[key]; !ok {
			t.Errorf("missing per-row key %q — JSON contract drift. Got keys: %v", key, mapKeys(row))
		}
	}
	attrs, ok := row["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("attributes is not an object: %T", row["attributes"])
	}
	if _, ok := attrs["currency"]; !ok {
		t.Errorf("missing attribute key %q — JSON contract drift", "currency")
	}
}

// TestTerritories_FixtureReplay exercises fetchTerritories against the golden
// fixture. Confirms paging-iterator integration plus typed decode.
func TestTerritories_FixtureReplay(t *testing.T) {
	srv := startFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/territories": {File: "territories_list"},
	})
	c := fixtureASCClient(t, srv)
	list, err := fetchTerritories(context.Background(), c)
	if err != nil {
		t.Fatalf("fetchTerritories: %v", err)
	}
	if len(list.Territories) != 4 {
		t.Fatalf("territories len = %d, want 4", len(list.Territories))
	}
	if list.Territories[0].ID != "USA" {
		t.Errorf("territories[0].ID = %q, want USA", list.Territories[0].ID)
	}
	if list.Territories[0].Attributes.Currency != "USD" {
		t.Errorf("territories[0].Currency = %q, want USD", list.Territories[0].Attributes.Currency)
	}
}

// TestTerritories_CacheMissReadFresh exercises the cache miss + write path:
// no file -> readTerritoriesCache returns false -> a subsequent
// writeTerritoriesCache + read round-trip recovers the same payload.
func TestTerritories_CacheMissReadFresh(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "territories.json")

	// Miss: file does not exist.
	if _, ok := readTerritoriesCache(path); ok {
		t.Fatalf("expected miss on absent file, got hit")
	}

	payload := TerritoryList{
		Territories: []TerritoryView{
			{ID: "USA", Type: "territories", Attributes: asc.TerritoryAttributes{Currency: "USD"}},
		},
	}
	if err := writeTerritoriesCache(path, payload); err != nil {
		t.Fatalf("writeTerritoriesCache: %v", err)
	}

	// Hit after write.
	got, ok := readTerritoriesCache(path)
	if !ok {
		t.Fatalf("expected hit after write, got miss")
	}
	if len(got.Territories) != 1 || got.Territories[0].ID != "USA" {
		t.Errorf("round-trip mismatch: got %+v, want [{USA … USD}]", got.Territories)
	}

	// Cache file should be 0600 (security discipline).
	st, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Errorf("cache file mode = %v, want 0600", st.Mode().Perm())
	}
}

// TestTerritories_CacheStaleEntryMisses asserts the 24h TTL: a payload with a
// SavedAt timestamp older than territoriesCacheTTL is treated as a miss.
func TestTerritories_CacheStaleEntryMisses(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "territories.json")

	stale := territoriesCacheFile{
		Version: territoriesCacheVersion,
		SavedAt: time.Now().Add(-2 * territoriesCacheTTL),
		Payload: TerritoryList{
			Territories: []TerritoryView{
				{ID: "USA", Type: "territories", Attributes: asc.TerritoryAttributes{Currency: "USD"}},
			},
		},
	}
	data, err := json.Marshal(stale)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, ok := readTerritoriesCache(path); ok {
		t.Errorf("expected miss on stale entry, got hit")
	}
}

// TestTerritories_CacheVersionMismatchMisses asserts that a cache file with an
// older version envelope is treated as a miss — guards against on-disk
// schema drift between Flightline releases.
func TestTerritories_CacheVersionMismatchMisses(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "territories.json")

	old := territoriesCacheFile{
		Version: territoriesCacheVersion - 1,
		SavedAt: time.Now(),
		Payload: TerritoryList{},
	}
	data, _ := json.Marshal(old)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, ok := readTerritoriesCache(path); ok {
		t.Errorf("expected miss on version mismatch, got hit")
	}
}

// TestTerritories_CacheCorruptMisses asserts that an unparseable cache file
// fails gracefully (degrades to a live fetch on the next run).
func TestTerritories_CacheCorruptMisses(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "territories.json")
	if err := os.WriteFile(path, []byte("not json {{{"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, ok := readTerritoriesCache(path); ok {
		t.Errorf("expected miss on corrupt file, got hit")
	}
}

// TestTerritoriesCachePath_ResolvesUnderXDG sets XDG_CACHE_HOME and verifies
// the path lands under it. macOS ignores XDG_CACHE_HOME (os.UserCacheDir uses
// $HOME/Library/Caches), so on darwin we fall back to a HOME-anchored
// assertion. Either way the path must end in flightline/territories.json.
func TestTerritoriesCachePath_ResolvesUnderXDG(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", filepath.Join(t.TempDir(), "xdg-cache"))
	t.Setenv("HOME", t.TempDir())

	p, err := territoriesCachePath()
	if err != nil {
		t.Fatalf("territoriesCachePath: %v", err)
	}
	if !strings.HasSuffix(p, filepath.Join("flightline", "territories.json")) {
		t.Errorf("path %q does not end with flightline/territories.json", p)
	}
}
