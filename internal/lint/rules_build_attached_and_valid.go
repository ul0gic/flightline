package lint

import (
	"fmt"
	"strings"

	"github.com/ul0gic/skipper/internal/asc"
)

// buildAttachedAndValidRule fires when the version under preflight either
// has no build attached or has a build whose processingState is not VALID.
// Apple blocks submission until both conditions are met:
//   - the version's `build` relationship resolves (data block present),
//   - the resolved Build.attributes.processingState == "VALID".
//
// Mode=Live only — the build's processing state is a wire-only concept;
// nothing in the authored YAML can determine VALID-ness ahead of time.
type buildAttachedAndValidRule struct{}

func init() { Register(buildAttachedAndValidRule{}) }

func (buildAttachedAndValidRule) ID() string         { return "build.attached-and-valid" }
func (buildAttachedAndValidRule) Severity() Severity { return SeverityError }
func (buildAttachedAndValidRule) Mode() Mode         { return ModeLive }

func (r buildAttachedAndValidRule) Check(ctx CheckContext) []Diagnostic {
	if !ctx.Live || ctx.Client == nil || ctx.BundleID == "" {
		return nil
	}
	appID, err := iapResolveAppID(ctx, ctx.BundleID)
	if err != nil {
		return []Diagnostic{r.fetchErr("resolve app", err)}
	}
	versionID, err := resolveVersionIDOnApp(ctx, appID, ctx.Version)
	if err != nil {
		return []Diagnostic{r.fetchErr("resolve version", err)}
	}

	resp, err := asc.Get[asc.Single[asc.BuildAttributes]](
		ctx.Ctx, ctx.Client, "/v1/appStoreVersions/"+versionID+"/build", nil,
	)
	if err != nil {
		// 404 here means "no build attached" — that's a rule violation, not
		// an infrastructure error. Apple sometimes returns 200 with empty
		// data instead; both shapes are handled here.
		if strings.Contains(err.Error(), "404") || strings.Contains(err.Error(), "NOT_FOUND") {
			return []Diagnostic{r.notAttachedDiag(ctx)}
		}
		return []Diagnostic{r.fetchErr("fetch attached build", err)}
	}
	if resp.Data.ID == "" {
		return []Diagnostic{r.notAttachedDiag(ctx)}
	}

	state := resp.Data.Attributes.ProcessingState
	if state == "VALID" {
		return nil
	}
	return []Diagnostic{{
		RuleID:   r.ID(),
		Severity: SeverityError,
		Message: fmt.Sprintf(
			"build %q is attached but processingState=%q (must be VALID)",
			resp.Data.Attributes.Version, state,
		),
		Path: "/spec/build",
		FixHint: "wait for Apple to finish processing (PROCESSING -> VALID), or re-upload " +
			"if the build is FAILED/INVALID. `skipper builds list <bundleId>` shows current state.",
		Reference: "PRD §L3 — build.attached-and-valid",
	}}
}

func (r buildAttachedAndValidRule) notAttachedDiag(ctx CheckContext) Diagnostic {
	return Diagnostic{
		RuleID:   r.ID(),
		Severity: SeverityError,
		Message:  fmt.Sprintf("version %q has no build attached", ctx.Version),
		Path:     "/spec/build",
		FixHint: fmt.Sprintf(
			"upload via Xcode/altool, wait for VALID, then `skipper builds attach %s --version %s --build <n>`.",
			ctx.BundleID, ctx.Version,
		),
		Reference: "PRD §L3 — build.attached-and-valid",
	}
}

func (r buildAttachedAndValidRule) fetchErr(what string, err error) Diagnostic {
	return Diagnostic{
		RuleID:   r.ID(),
		Severity: SeverityError,
		Message:  fmt.Sprintf("%s: %v", what, err),
		FixHint:  "rerun preflight; if it persists check ASC API access and rate-limit headroom.",
	}
}
