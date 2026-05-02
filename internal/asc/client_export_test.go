package asc

// SetBaseURL is a test-only escape hatch that overrides the configured
// hardcoded baseURL with one that points at an httptest.Server. The Go
// `*_export_test.go` convention exports an unexported field's setter to
// callers in *external* test packages without leaking the field to
// production consumers.
//
// Use only from tests. The hardcoded production baseURL stays the single
// source of truth at runtime.
func (c *Client) SetBaseURL(u string) {
	c.baseURL = u
}
