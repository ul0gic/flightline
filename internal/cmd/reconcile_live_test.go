//go:build integration

package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ul0gic/flightline/internal/asc"
	"github.com/ul0gic/flightline/internal/config"
	"github.com/ul0gic/flightline/internal/plan"
	"github.com/ul0gic/flightline/internal/state"
)

const c3bCopyrightPath = "/spec/version/copyright"

func TestC3B_LiveCopyrightReconcile(t *testing.T) {
	env := c3bEnvironment()
	if env["FLIGHTLINE_LIVE_OPT_IN"] == "" {
		t.Skip("set explicit FLIGHTLINE_LIVE_OPT_IN and exact app/version allowlist for live reconciliation")
	}
	target, err := c3bValidateTarget(env)
	if err != nil {
		t.Fatal(err)
	}
	if err := c3bRunLive(target); err != nil {
		t.Fatal(err)
	}
}

func c3bRunLive(target c3bTarget) error {
	budget := &c3bBudgetTransport{base: http.DefaultTransport}
	c, err := c3bClient(target, budget)
	if err != nil {
		return err
	}
	forwardCtx, forwardCancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer forwardCancel()
	_, baseline, err := c3bReadCopyright(forwardCtx, c, target)
	if err != nil {
		return fmt.Errorf("baseline read failed before mutation: %w", err)
	}
	if baseline == "" {
		return errors.New("baseline copyright is absent; no reversible nonempty value to restore")
	}
	appID, err := resolveAppID(forwardCtx, c, target.BundleID)
	if err != nil {
		return fmt.Errorf("resolve exact app: %w", err)
	}
	view, err := lookupVersion(forwardCtx, c, appID, target.Version, target.Platform)
	if err != nil {
		return fmt.Errorf("resolve exact version before mutation: %w", err)
	}
	if view == nil || view.ID == "" || view.Attributes.VersionString != target.Version || view.Attributes.Platform != target.Platform {
		return errors.New("version lookup did not return the exact allowlisted version and platform")
	}
	marker := " [flightline live check]"
	if strings.Contains(baseline, marker) {
		return errors.New("baseline already contains the live marker; resolve previous run before mutating")
	}
	if len([]rune(baseline))+len([]rune(marker)) > 100 {
		return errors.New("baseline copyright leaves no room for live marker within 100 characters")
	}
	changed := baseline + marker
	budget.setIntent(view.ID, baseline, changed)
	now := time.Now().UTC()
	m := c3bManifest{
		SchemaVersion: 1, BundleID: target.BundleID, Version: target.Version,
		Platform: target.Platform, Field: c3bCopyrightPath, Baseline: baseline,
		Target: changed, Status: "baseline-captured", CapturedAt: now, UpdatedAt: now,
	}
	if err := c3bCreateManifest(target.ManifestPath, m); err != nil {
		return err
	}
	forwardErr := c3bForward(forwardCtx, c, target, &m)
	forwardCancel()
	budget.setRestoring()
	restoreCtx, restoreCancel := context.WithTimeout(context.Background(), c3bTotalTime-5*time.Minute)
	defer restoreCancel()
	restoreErr := c3bRestore(restoreCtx, c, target, &m)
	if restoreErr != nil {
		return errors.Join(forwardErr, fmt.Errorf("restoration unverified; manifest %s: %w", target.ManifestPath, restoreErr))
	}
	return forwardErr
}

func c3bForward(ctx context.Context, c *asc.Client, target c3bTarget, m *c3bManifest) error {
	if err := c3bSetStatus(target.ManifestPath, m, "forward-mutation-pending", ""); err != nil {
		return err
	}
	if err := c3bApplyCopyright(ctx, c, target, m.Baseline, m.Target); err != nil {
		return fmt.Errorf("forward apply outcome uncertain; fresh read required: %w", err)
	}
	if err := c3bSetStatus(target.ManifestPath, m, "forward-applied", ""); err != nil {
		return err
	}
	live, value, err := c3bReadCopyright(ctx, c, target)
	if err != nil {
		return fmt.Errorf("forward refetch failed: %w", err)
	}
	if value != m.Target {
		return fmt.Errorf("forward refetch differs from target; observed copyright %q", value)
	}
	if err := c3bAssertEmptyPlan(m.Target, live); err != nil {
		return err
	}
	return c3bSetStatus(target.ManifestPath, m, "forward-converged", "")
}

func c3bRestore(ctx context.Context, c *asc.Client, target c3bTarget, m *c3bManifest) error {
	_, value, err := c3bReadCopyright(ctx, c, target)
	if err != nil {
		_ = c3bSetStatus(target.ManifestPath, m, "restoration-unverified", "fresh read failed before restoration")
		return fmt.Errorf("fresh read before restoration: %w", err)
	}
	if value != m.Baseline {
		if value != m.Target {
			_ = c3bSetStatus(target.ManifestPath, m, "restoration-unverified", "live value differs from baseline and harness target")
			return fmt.Errorf("live value changed externally; observed copyright %q", value)
		}
		if err := c3bSetStatus(target.ManifestPath, m, "restore-mutation-pending", ""); err != nil {
			return err
		}
		if err := c3bApplyCopyright(ctx, c, target, m.Target, m.Baseline); err != nil {
			m.Note = "restore apply response uncertain; verify by fresh read"
		}
	}
	live, value, err := c3bReadCopyright(ctx, c, target)
	if err != nil {
		_ = c3bSetStatus(target.ManifestPath, m, "restoration-unverified", "post-restore read failed")
		return fmt.Errorf("post-restore read: %w", err)
	}
	if value != m.Baseline {
		_ = c3bSetStatus(target.ManifestPath, m, "restoration-unverified", "post-restore value differs from baseline")
		return fmt.Errorf("post-restore copyright differs from baseline; observed %q", value)
	}
	if err := c3bAssertEmptyPlan(m.Baseline, live); err != nil {
		return err
	}
	return c3bSetStatus(target.ManifestPath, m, "restored-and-verified", m.Note)
}

func c3bReadCopyright(ctx context.Context, c *asc.Client, target c3bTarget) (*config.State, string, error) {
	live, err := state.Fetch(ctx, c, target.BundleID, state.FetchOpts{
		Version: target.Version, Platform: target.Platform, RequireEditable: true,
	})
	if err != nil {
		return nil, "", err
	}
	if live.Metadata.BundleID != target.BundleID || live.Metadata.Version != target.Version || live.Metadata.Platform != target.Platform {
		return nil, "", errors.New("fresh snapshot identity differs from exact allowlisted target")
	}
	if live.Spec.Version == nil || live.Spec.Version.Copyright == nil {
		return live, "", nil
	}
	return live, *live.Spec.Version.Copyright, nil
}

func c3bApplyCopyright(ctx context.Context, c *asc.Client, target c3bTarget, from, to string) error {
	desired := &config.State{Spec: config.StateSpec{Version: &config.VersionSpec{Copyright: &to}}}
	live := &config.State{Spec: config.StateSpec{Version: &config.VersionSpec{Copyright: &from}}}
	changes := plan.Diff(desired, live)
	if len(changes) != 1 || changes[0].Path != c3bCopyrightPath || changes[0].Op != plan.OpUpdate {
		return errors.New("copyright intent did not produce exactly one update")
	}
	result, err := state.Apply(ctx, c, changes, state.ApplyOpts{
		Confirm: true,
		Context: state.ApplyContext{BundleID: target.BundleID, Version: target.Version, Platform: target.Platform, StateDir: os.TempDir()},
	})
	if err != nil {
		return err
	}
	if result == nil || len(result.Applied) != 1 || len(result.Errors) != 0 || len(result.Skipped) != 0 {
		return errors.New("apply did not report exactly one successful copyright update")
	}
	return nil
}

func c3bAssertEmptyPlan(desiredValue string, live *config.State) error {
	desired := &config.State{Spec: config.StateSpec{Version: &config.VersionSpec{Copyright: &desiredValue}}}
	if changes := plan.Diff(desired, live); len(changes) != 0 {
		return fmt.Errorf("copyright plan is not empty: %d changes", len(changes))
	}
	return nil
}

func c3bSetStatus(path string, m *c3bManifest, status, note string) error {
	m.Status = status
	m.Note = strings.TrimSpace(note)
	return c3bUpdateManifest(path, *m)
}
