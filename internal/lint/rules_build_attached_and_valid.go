package lint

import (
	"fmt"
	"strings"

	"github.com/ul0gic/flightline/internal/asc"
)

// buildAttachedAndValidRule fires when no build is attached or processingState != "VALID".
// Apple blocks submission until the build relationship resolves and processingState == "VALID" (live-only).
type buildAttachedAndValidRule struct{}

func init() { Register(buildAttachedAndValidRule{}) }

func (buildAttachedAndValidRule) ID() string         { return "build.attached-and-valid" }
func (buildAttachedAndValidRule) Severity() Severity { return SeverityError }
func (buildAttachedAndValidRule) Mode() Mode         { return ModeLive }
func (buildAttachedAndValidRule) Doc() string {
	return "Checks that the App Store version has a build attached and that the build's processingState is VALID. " +
		"Apple's submission flow hard-blocks Submit for Review until both hold, but the block only surfaces at submit time after you have confirmed the dialog, so the gap is easy to miss until the worst moment. " +
		"Fix it by uploading the build, waiting for processing to reach VALID, then attaching it to the version."
}

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
		// 404 means "no build attached" (a rule violation); Apple sometimes returns 200+empty data instead.
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
			"if the build is FAILED/INVALID. `flightline builds list <bundleId>` shows current state.",
		Reference: "PRD §L3: build.attached-and-valid",
	}}
}

func (r buildAttachedAndValidRule) notAttachedDiag(ctx CheckContext) Diagnostic {
	return Diagnostic{
		RuleID:   r.ID(),
		Severity: SeverityError,
		Message:  fmt.Sprintf("version %q has no build attached", ctx.Version),
		Path:     "/spec/build",
		FixHint: fmt.Sprintf(
			"upload via Xcode/altool, wait for VALID, then `flightline builds attach %s --version %s --build <n>`.",
			ctx.BundleID, ctx.Version,
		),
		Reference: "PRD §L3: build.attached-and-valid",
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
