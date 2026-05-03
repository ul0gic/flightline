package asc

// Diagnostics read surface. Apple deduplicates crash and hang reports into
// "diagnostic signatures" — same call stack, same crash, regardless of how
// many users hit it. Each signature carries a weight (severity / occurrence
// proxy) and an optional insight payload contrasting it against prior
// versions.
//
// Apple v4.3 only exposes diagnostic signatures scoped to a build:
//
//	GET /v1/builds/{id}/diagnosticSignatures   — list signatures for a build
//	GET /v1/diagnosticSignatures/{id}/logs     — full call-stack logs
//
// There is NO /v1/diagnosticSignatures global list in v4.3 and NO
// /v1/diagnosticSignatures/{id} get. The `diagnostics get` command in
// Flightline resolves to the /logs endpoint instead.
//
// Source:
//
//	jq '.components.schemas.DiagnosticSignature.properties.attributes.properties' openapi.oas.json
//	jq '.components.schemas.DiagnosticInsight' openapi.oas.json
//	jq '.components.schemas.diagnosticLogs' openapi.oas.json

// Apple-defined diagnostic types. Surfaced as named constants so command
// code can filter / compare without typos.
const (
	DiagnosticTypeDiskWrites = "DISK_WRITES"
	DiagnosticTypeHangs      = "HANGS"
	DiagnosticTypeLaunches   = "LAUNCHES"
)

// DiagnosticInsight is Apple's regression-direction wrapper. Apple compares
// the current build's metric against prior versions and reports whether the
// metric is regressing or improving along a category.
type DiagnosticInsight struct {
	InsightType       string                       `json:"insightType,omitempty"`
	Direction         string                       `json:"direction,omitempty"`
	ReferenceVersions []DiagnosticReferenceVersion `json:"referenceVersions,omitempty"`
}

// DiagnosticReferenceVersion is one prior-version data point Apple includes
// in the insight payload (e.g. "1.0.0 had value 1.42").
type DiagnosticReferenceVersion struct {
	Version string  `json:"version,omitempty"`
	Value   float64 `json:"value,omitempty"`
}

// DiagnosticSignatureAttributes is the subset of Apple's
// DiagnosticSignature.attributes Flightline reads. Weight is Apple's severity
// proxy — higher weight = more user impact / more occurrences.
type DiagnosticSignatureAttributes struct {
	DiagnosticType string             `json:"diagnosticType,omitempty"`
	Signature      string             `json:"signature,omitempty"`
	Weight         float64            `json:"weight,omitempty"`
	Insight        *DiagnosticInsight `json:"insight,omitempty"`
}

// DiagnosticLogsResponse is the shape Apple returns from
// /v1/diagnosticSignatures/{id}/logs. The response is NOT a JSON:API
// envelope (no Resource[T]/Collection[T]); it's a custom productData /
// version structure with embedded call-stack trees.
type DiagnosticLogsResponse struct {
	ProductData []DiagnosticProductData `json:"productData,omitempty"`
	Version     string                  `json:"version,omitempty"`
}

// DiagnosticProductData is one signature's group of insights and logs.
type DiagnosticProductData struct {
	SignatureID        string                   `json:"signatureId,omitempty"`
	DiagnosticInsights []DiagnosticLogInsight   `json:"diagnosticInsights,omitempty"`
	DiagnosticLogs     []DiagnosticLogStackTree `json:"diagnosticLogs,omitempty"`
}

// DiagnosticLogInsight is a single insight entry from the logs response.
// The wire shape differs slightly from the signature-level insight; we keep
// them separate to match Apple's casing.
type DiagnosticLogInsight struct {
	InsightsURL      string `json:"insightsURL,omitempty"`
	InsightsCategory string `json:"insightsCategory,omitempty"`
	InsightsString   string `json:"insightsString,omitempty"`
}

// DiagnosticLogStackTree is one call-stack tree with the metadata Apple
// captured at crash time. The full call stack lives under CallStackTree
// as nested CallStacks → CallStackRootFrames; we surface it as raw
// JSON-shaped maps to avoid building a deeply-nested typed model that
// changes shape with every Apple revision.
type DiagnosticLogStackTree struct {
	CallStackTree      []DiagnosticCallStackTreeBranch `json:"callStackTree,omitempty"`
	DiagnosticMetaData DiagnosticLogMetaData           `json:"diagnosticMetaData,omitempty"`
}

// DiagnosticCallStackTreeBranch is one branch of the call-stack tree.
type DiagnosticCallStackTreeBranch struct {
	CallStackPerThread bool                      `json:"callStackPerThread,omitempty"`
	CallStacks         []DiagnosticCallStackBlob `json:"callStacks,omitempty"`
}

// DiagnosticCallStackBlob carries the actual frame nodes. The frame node
// tree is recursive; we use json.RawMessage there so unmarshal succeeds
// against whatever shape Apple ships.
type DiagnosticCallStackBlob struct {
	CallStackRootFrames []map[string]any `json:"callStackRootFrames,omitempty"`
}

// DiagnosticLogMetaData is the crash-time environment Apple records.
type DiagnosticLogMetaData struct {
	BundleID             string `json:"bundleId,omitempty"`
	Event                string `json:"event,omitempty"`
	OsVersion            string `json:"osVersion,omitempty"`
	AppVersion           string `json:"appVersion,omitempty"`
	WritesCaused         string `json:"writesCaused,omitempty"`
	DeviceType           string `json:"deviceType,omitempty"`
	PlatformArchitecture string `json:"platformArchitecture,omitempty"`
	EventDetail          string `json:"eventDetail,omitempty"`
	BuildVersion         string `json:"buildVersion,omitempty"`
}
