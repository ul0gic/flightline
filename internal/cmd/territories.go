package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
	"github.com/ul0gic/flightline/internal/asc"
)

type TerritoryView struct {
	ID         string                  `json:"id"`
	Type       string                  `json:"type"`
	Attributes asc.TerritoryAttributes `json:"attributes"`
}

type TerritoryList struct {
	Territories []TerritoryView `json:"territories"`
}

func (l TerritoryList) TableRows() (headers []string, rows [][]string) {
	headers = []string{"TERRITORY", "CURRENCY"}
	rows = make([][]string, 0, len(l.Territories))
	for i := range l.Territories {
		t := &l.Territories[i]
		rows = append(rows, []string{t.ID, t.Attributes.Currency})
	}
	return headers, rows
}

const territoriesCacheTTL = 24 * time.Hour

// Bump when the cached payload shape changes; older caches become a miss.
const territoriesCacheVersion = 1

type territoriesCacheFile struct {
	Version int           `json:"version"`
	SavedAt time.Time     `json:"savedAt"`
	Payload TerritoryList `json:"payload"`
}

var territoriesCmd = &cobra.Command{
	Use:   "territories",
	Short: "List App Store territories",
	Long: `territories groups read commands over the /v1/territories resource.

Apple's territory list is reference data: the same set across every ASC
account, with currency codes that change at most a few times a year. The
list command caches results under $XDG_CACHE_HOME/flightline/territories.json
for 24 hours by default; pass --no-cache to force a fresh fetch.`,
}

var territoriesListCmd = &cobra.Command{
	Use:          "list",
	Short:        "List App Store territories with their ISO 4217 currency codes",
	SilenceUsage: true,
	Args:         cobra.NoArgs,
	RunE:         runTerritoriesList,
	Example: `  flightline territories list
  flightline territories list --output json | jq -r '.territories[].id'
  flightline territories list --no-cache`,
}

var territoriesListNoCache bool

func init() {
	territoriesListCmd.Flags().BoolVar(&territoriesListNoCache, "no-cache", false, "force a fresh fetch (bypasses the 24h cache)")

	territoriesCmd.AddCommand(territoriesListCmd)
	rootCmd.AddCommand(territoriesCmd)
}

func runTerritoriesList(cmd *cobra.Command, _ []string) error {
	cachePath, err := territoriesCachePath()
	if err != nil {
		// Non-fatal: live fetch still proceeds, cache just isn't saved.
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "flightline: territories cache disabled: %v\n", err)
		cachePath = ""
	}

	if !territoriesListNoCache && cachePath != "" {
		if list, ok := readTerritoriesCache(cachePath); ok {
			return Render(list, outputMode())
		}
	}

	c, err := newClient()
	if err != nil {
		return err
	}

	list, err := fetchTerritories(cmd.Context(), c)
	if err != nil {
		return err
	}

	if cachePath != "" {
		// Non-fatal: fresh data still goes to stdout, only the next-run
		// speedup is lost.
		if werr := writeTerritoriesCache(cachePath, list); werr != nil {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "flightline: territories cache write failed: %v\n", werr)
		}
	}

	return Render(list, outputMode())
}

// 200 is Apple's max page size.
func fetchTerritories(ctx context.Context, c *asc.Client) (TerritoryList, error) {
	out := make([]TerritoryView, 0, 200)
	for page, err := range asc.Pages[asc.TerritoryAttributes](ctx, c, "/v1/territories", nil) {
		if err != nil {
			return TerritoryList{}, err
		}
		for _, r := range page.Data {
			out = append(out, TerritoryView{ID: r.ID, Type: r.Type, Attributes: r.Attributes})
		}
	}
	return TerritoryList{Territories: out}, nil
}

func territoriesCachePath() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("resolve cache dir: %w", err)
	}
	return filepath.Join(dir, "flightline", "territories.json"), nil
}

// Errors are swallowed: every miss reason (absent, corrupt, version
// mismatch, stale) degrades to a live fetch, invisible to the user.
func readTerritoriesCache(path string) (TerritoryList, bool) {
	data, err := os.ReadFile(path) // #nosec G304 -- path is computed from os.UserCacheDir()
	if err != nil {
		return TerritoryList{}, false
	}
	var f territoriesCacheFile
	if err := json.Unmarshal(data, &f); err != nil {
		return TerritoryList{}, false
	}
	if f.Version != territoriesCacheVersion {
		return TerritoryList{}, false
	}
	if time.Since(f.SavedAt) > territoriesCacheTTL {
		return TerritoryList{}, false
	}
	return f.Payload, true
}

// Tmp-file + atomic rename so a Ctrl-C mid-write can't corrupt the cache.
func writeTerritoriesCache(path string, payload TerritoryList) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create cache dir: %w", err)
	}
	f := territoriesCacheFile{
		Version: territoriesCacheVersion,
		SavedAt: time.Now().UTC(),
		Payload: payload,
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal cache: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".territories-*.json.tmp")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpPath) }
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("write temp: %w", err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("chmod temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close temp: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		cleanup()
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}
