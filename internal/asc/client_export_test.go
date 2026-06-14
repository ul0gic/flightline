package asc

// SetBaseURL points the client at an httptest.Server. Test-only: the
// hardcoded production baseURL stays the single source of truth at runtime.
func (c *Client) SetBaseURL(u string) {
	c.baseURL = u
}
