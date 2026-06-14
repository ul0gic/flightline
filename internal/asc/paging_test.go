package asc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestPages_FollowsNextLink(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/apps":
			if r.URL.Query().Get("cursor") == "page2" {
				_, _ = fmt.Fprint(w, `{"data":[{"type":"apps","id":"3","attributes":{"bundleId":"com.c"}},{"type":"apps","id":"4","attributes":{"bundleId":"com.d"}}],"links":{"self":""}}`)
				return
			}
			next := srv.URL + "/v1/apps?cursor=page2"
			payload := map[string]any{
				"data": []map[string]any{
					{"type": "apps", "id": "1", "attributes": map[string]any{"bundleId": "com.a"}},
					{"type": "apps", "id": "2", "attributes": map[string]any{"bundleId": "com.b"}},
				},
				"links": map[string]any{
					"self": "",
					"next": next,
				},
			}
			_ = json.NewEncoder(w).Encode(payload)
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	var ids []string
	for page, err := range Pages[appAttrs](context.Background(), c, "/v1/apps", url.Values{"limit": {"2"}}) {
		if err != nil {
			t.Fatalf("page err: %v", err)
		}
		for _, r := range page.Data {
			ids = append(ids, r.ID)
		}
	}
	want := []string{"1", "2", "3", "4"}
	if strings.Join(ids, ",") != strings.Join(want, ",") {
		t.Errorf("ids = %v, want %v", ids, want)
	}
}

func TestPages_StopsOnFalsyYield(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"data":[{"type":"apps","id":"1"}],"links":{"self":"","next":"https://api.appstoreconnect.apple.com/v1/apps?cursor=2"}}`)
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	var seen int
	for _, err := range Pages[appAttrs](context.Background(), c, "/v1/apps", nil) {
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		seen++
		break
	}
	if seen != 1 {
		t.Errorf("seen = %d, want 1 (early break)", seen)
	}
}

func TestPages_PropagatesError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"errors":[{"code":"NOT_AUTHORIZED","title":"x","detail":"y","status":"401"}]}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	var sawErr bool
	for _, err := range Pages[appAttrs](context.Background(), c, "/v1/apps", nil) {
		if err != nil {
			sawErr = true
			if !errors.Is(err, ErrUnauthorized) {
				t.Errorf("err = %v, want ErrUnauthorized", err)
			}
		}
	}
	if !sawErr {
		t.Error("Pages did not yield error")
	}
}

func TestStripBase(t *testing.T) {
	got := stripBase("https://api.appstoreconnect.apple.com/v1/apps?cursor=x", "https://api.appstoreconnect.apple.com")
	if got != "/v1/apps?cursor=x" {
		t.Errorf("stripBase = %q", got)
	}
	if got := stripBase("https://attacker.example.com/v1/apps", "https://api.appstoreconnect.apple.com"); got != "https://attacker.example.com/v1/apps" {
		t.Errorf("stripBase = %q", got)
	}
}

// TestPages_FollowsNextLink_FromFixtures exercises absolute-URL → strip-base → re-fetch
// end-to-end by rewriting the fixture's links.next host to the test server.
func TestPages_FollowsNextLink_FromFixtures(t *testing.T) {
	page1Bytes, err := readFixture("apps_list_paginated_page1")
	if err != nil {
		t.Fatalf("read page1: %v", err)
	}
	page2Bytes, err := readFixture("apps_list_paginated_page2")
	if err != nil {
		t.Fatalf("read page2: %v", err)
	}

	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/apps":
			if r.URL.Query().Get("cursor") == "PAGE2_CURSOR" {
				_, _ = w.Write(page2Bytes)
				return
			}
			rewritten := strings.ReplaceAll(
				string(page1Bytes),
				"https://api.appstoreconnect.apple.com",
				srv.URL,
			)
			_, _ = io.WriteString(w, rewritten)
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	c := newTestClient(t, srv)

	var (
		pageCount int
		ids       []string
	)
	for page, perr := range Pages[appAttrs](context.Background(), c, "/v1/apps", url.Values{"limit": {"2"}}) {
		if perr != nil {
			t.Fatalf("page err: %v", perr)
		}
		pageCount++
		for _, r := range page.Data {
			ids = append(ids, r.ID)
		}
	}

	if pageCount != 2 {
		t.Errorf("pageCount = %d, want 2", pageCount)
	}
	wantIDs := []string{"1234567890", "1234567891", "1234567892"}
	if strings.Join(ids, ",") != strings.Join(wantIDs, ",") {
		t.Errorf("ids = %v, want %v", ids, wantIDs)
	}
}

// TestPages_RejectsForeignHostInNextLink guards against JWT exfiltration: a links.next
// pointing off-host must error rather than send a credentialed request to that host.
func TestPages_RejectsForeignHostInNextLink(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{
			"data":[{"type":"apps","id":"1","attributes":{"bundleId":"com.example.alpha"}}],
			"links":{"self":"","next":"https://attacker.example.com/v1/apps?cursor=stolen"}
		}`)
	}))
	defer srv.Close()

	c := newTestClient(t, srv)

	var (
		pages   int
		sawErr  bool
		gotErr  error
		recvIDs []string
	)
	for page, perr := range Pages[appAttrs](context.Background(), c, "/v1/apps", nil) {
		if perr != nil {
			sawErr = true
			gotErr = perr
			continue
		}
		pages++
		for _, r := range page.Data {
			recvIDs = append(recvIDs, r.ID)
		}
	}

	if pages != 1 {
		t.Errorf("pages = %d, want 1 (foreign-host rejection should stop after page 1)", pages)
	}
	if !sawErr {
		t.Fatal("Pages did not yield error on foreign-host next link")
	}
	if !strings.Contains(gotErr.Error(), "foreign host") {
		t.Errorf("err = %v, want substring 'foreign host'", gotErr)
	}
	// Rejection kicks in on the follow-up, so page 1 data must remain observable.
	if len(recvIDs) != 1 || recvIDs[0] != "1" {
		t.Errorf("recvIDs = %v, want [1] (page 1 data should be intact)", recvIDs)
	}
}
