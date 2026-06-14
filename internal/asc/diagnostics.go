package asc

// Apple-defined diagnostic types. Surfaced as named constants so command
// code can filter / compare without typos.
const (
	DiagnosticTypeDiskWrites = "DISK_WRITES"
	DiagnosticTypeHangs      = "HANGS"
	DiagnosticTypeLaunches   = "LAUNCHES"
)

// DiagnosticInsight is Apple's regression-direction wrapper comparing the current build's metric against prior versions.
type DiagnosticInsight struct {
	InsightType       string                       `json:"insightType,omitempty"`
	Direction         string                       `json:"direction,omitempty"`
	ReferenceVersions []DiagnosticReferenceVersion `json:"referenceVersions,omitempty"`
}

// DiagnosticReferenceVersion is one prior-version data point in the insight payload (e.g. "1.0.0 had value 1.42").
type DiagnosticReferenceVersion struct {
	Version string  `json:"version,omitempty"`
	Value   float64 `json:"value,omitempty"`
}

// DiagnosticSignatureAttributes is the subset of Apple's DiagnosticSignature.attributes Flightline reads.
// Weight is Apple's severity proxy: higher weight = more user impact / occurrences.
type DiagnosticSignatureAttributes struct {
	DiagnosticType string             `json:"diagnosticType,omitempty"`
	Signature      string             `json:"signature,omitempty"`
	Weight         float64            `json:"weight,omitempty"`
	Insight        *DiagnosticInsight `json:"insight,omitempty"`
}

// DiagnosticLogsResponse is the shape Apple returns from /v1/diagnosticSignatures/{id}/logs.
// NOT a JSON:API envelope: it's a custom productData/version structure with embedded call-stack trees.
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
// Wire shape differs from the signature-level DiagnosticInsight; kept separate to match Apple's casing.
type DiagnosticLogInsight struct {
	InsightsURL      string `json:"insightsURL,omitempty"`
	InsightsCategory string `json:"insightsCategory,omitempty"`
	InsightsString   string `json:"insightsString,omitempty"`
}

// DiagnosticLogStackTree is one call-stack tree with crash-time metadata.
// CallStackTree uses raw maps to avoid a deeply-nested typed model that changes shape with Apple revisions.
type DiagnosticLogStackTree struct {
	CallStackTree      []DiagnosticCallStackTreeBranch `json:"callStackTree,omitempty"`
	DiagnosticMetaData DiagnosticLogMetaData           `json:"diagnosticMetaData,omitempty"`
}

// DiagnosticCallStackTreeBranch is one branch of the call-stack tree.
type DiagnosticCallStackTreeBranch struct {
	CallStackPerThread bool                      `json:"callStackPerThread,omitempty"`
	CallStacks         []DiagnosticCallStackBlob `json:"callStacks,omitempty"`
}

// DiagnosticCallStackBlob carries the actual frame nodes as raw maps; the tree is recursive and Apple's shape varies.
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
