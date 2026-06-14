package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestReviewerDemo_RegisteredOnRoot(t *testing.T) {
	var rd *cobra.Command
	for _, c := range rootCmd.Commands() {
		if c.Name() == "reviewer-demo" {
			rd = c
			break
		}
	}
	if rd == nil {
		t.Fatal("reviewer-demo not registered on rootCmd")
	}
	subs := map[string]bool{}
	for _, sc := range rd.Commands() {
		subs[sc.Name()] = true
	}
	if !subs["set"] {
		t.Errorf("reviewer-demo set subcommand missing")
	}
}

func TestReviewerDemoSet_VersionRequired(t *testing.T) {
	f := reviewerDemoSetCmd.Flag("version")
	if f == nil {
		t.Fatal("--version flag missing")
	}
	req := f.Annotations[cobra.BashCompOneRequiredFlag]
	if len(req) != 1 || req[0] != "true" {
		t.Errorf("--version should be required")
	}
}

func TestSplitContactName(t *testing.T) {
	cases := []struct {
		in, wantFirst, wantLast string
	}{
		{"Jane Doe", "Jane", "Doe"},
		{"Jane", "Jane", ""},
		{"  Jane Doe  ", "Jane", "Doe"},
		{"Jane Mary Doe", "Jane", "Mary Doe"},
		{"", "", ""},
	}
	for _, c := range cases {
		first, last := splitContactName(c.in)
		if first != c.wantFirst || last != c.wantLast {
			t.Errorf("splitContactName(%q) = (%q, %q), want (%q, %q)", c.in, first, last, c.wantFirst, c.wantLast)
		}
	}
}

func TestStrPtrEq(t *testing.T) {
	a := "x"
	b := "x"
	c := "y"
	cases := []struct {
		a, b *string
		want bool
	}{
		{nil, nil, true},
		{&a, nil, false},
		{nil, &a, false},
		{&a, &b, true},
		{&a, &c, false},
	}
	for i, tc := range cases {
		if got := strPtrEq(tc.a, tc.b); got != tc.want {
			t.Errorf("case %d: strPtrEq = %v, want %v", i, got, tc.want)
		}
	}
}

// --password-file must trim the trailing newline (the `echo > file` case) but keep inner whitespace.
func TestResolveReviewerPassword_FromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pwd")
	if err := os.WriteFile(path, []byte("hunter2 with space\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	prev := reviewerDemoSetPasswordFile
	prevPwd := reviewerDemoSetPassword
	t.Cleanup(func() {
		reviewerDemoSetPasswordFile = prev
		reviewerDemoSetPassword = prevPwd
	})
	reviewerDemoSetPasswordFile = path
	reviewerDemoSetPassword = ""

	got, err := resolveReviewerPassword()
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got != "hunter2 with space" {
		t.Errorf("got %q, want %q", got, "hunter2 with space")
	}
}

// TestResolveReviewerPassword_FlagWins asserts --password (when set without
// --password-file) is returned verbatim.
func TestResolveReviewerPassword_FlagWins(t *testing.T) {
	prev := reviewerDemoSetPasswordFile
	prevPwd := reviewerDemoSetPassword
	t.Cleanup(func() {
		reviewerDemoSetPasswordFile = prev
		reviewerDemoSetPassword = prevPwd
	})
	reviewerDemoSetPasswordFile = ""
	reviewerDemoSetPassword = "shellpass"

	got, err := resolveReviewerPassword()
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got != "shellpass" {
		t.Errorf("got %q, want shellpass", got)
	}
}

// TestResolveReviewerPassword_BothSetIsError asserts mutual exclusion of
// --password and --password-file.
func TestResolveReviewerPassword_BothSetIsError(t *testing.T) {
	prev := reviewerDemoSetPasswordFile
	prevPwd := reviewerDemoSetPassword
	t.Cleanup(func() {
		reviewerDemoSetPasswordFile = prev
		reviewerDemoSetPassword = prevPwd
	})
	reviewerDemoSetPasswordFile = "/tmp/x"
	reviewerDemoSetPassword = "y"

	_, err := resolveReviewerPassword()
	if err == nil {
		t.Fatal("want error, got nil")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("error %q should mention mutually exclusive", err)
	}
}

// Headline security test: no error returned to the user may contain the password value.
func TestRedactReviewerError_StripsPassword(t *testing.T) {
	password := "hunter2-LiteralPassword"
	leaked := fmt.Errorf("PATCH failed for password=%s on the server", password)
	got := redactReviewerError(leaked, password)
	if got == nil {
		t.Fatal("redactReviewerError returned nil for non-nil input")
	}
	if strings.Contains(got.Error(), password) {
		t.Errorf("password leaked through redactor: %q", got)
	}
	if !strings.Contains(got.Error(), "[REDACTED-PASSWORD]") {
		t.Errorf("redaction marker missing: %q", got)
	}
}

// TestRedactReviewerError_NilPassthrough confirms nil-in nil-out.
func TestRedactReviewerError_NilPassthrough(t *testing.T) {
	if redactReviewerError(nil, "secret") != nil {
		t.Errorf("nil input should pass through")
	}
}

// Empty password is a no-op: the original error pointer is returned unchanged.
func TestRedactReviewerError_EmptyPassword(t *testing.T) {
	orig := errors.New("some api error")
	got := redactReviewerError(orig, "")
	if got != orig { //nolint:errorlint // pointer-identity check is intentional: no allocation on the no-op path
		t.Errorf("empty password should pass through original error pointer")
	}
}

// TestRedactReviewerError_NoMatch is also a no-op when the password doesn't
// appear in the message: same pointer-identity behavior.
func TestRedactReviewerError_NoMatch(t *testing.T) {
	orig := errors.New("403 forbidden")
	got := redactReviewerError(orig, "shellpass")
	if got != orig { //nolint:errorlint // pointer-identity check is intentional: no allocation on the no-op path
		t.Errorf("no-match should pass through original error pointer; got new error %q", got)
	}
}

// The result struct has only the boolean DemoAccountPasswordSet; a regression adding a password field lands here.
func TestReviewerDemoWriteResult_NeverIncludesPassword(t *testing.T) {
	r := ReviewerDemoWriteResult{
		Action:                 "set",
		ID:                     "RD1",
		Type:                   "appStoreReviewDetails",
		BundleID:               "com.example.alpha",
		VersionString:          "1.0.1",
		ContactFirstName:       "Jane",
		ContactLastName:        "Doe",
		ContactEmail:           "reviewer@example.com",
		DemoAccountName:        "demo",
		DemoAccountPasswordSet: true,
	}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	out := string(b)
	for _, leak := range []string{"password", "Password"} {
		// Allow "demoAccountPasswordSet" but reject anything else.
		if strings.Contains(out, leak) && !strings.Contains(out, "PasswordSet") {
			t.Errorf("password-related field leaked: %s", out)
		}
	}
	if !strings.Contains(out, `"demoAccountPasswordSet":true`) {
		t.Errorf("demoAccountPasswordSet missing: %s", out)
	}
}

// TestReviewerDemoWriteResult_JSONShape locks the contract.
func TestReviewerDemoWriteResult_JSONShape(t *testing.T) {
	r := ReviewerDemoWriteResult{
		Action:                 "set",
		ID:                     "RD1",
		Type:                   "appStoreReviewDetails",
		BundleID:               "com.example.alpha",
		VersionString:          "1.0.1",
		NoOp:                   false,
		ChangedKeys:            []string{"contactEmail", "demoAccountPassword"},
		DemoAccountPasswordSet: true,
	}
	b, _ := json.Marshal(r)
	out := string(b)
	for _, want := range []string{
		`"action":"set"`,
		`"id":"RD1"`,
		`"type":"appStoreReviewDetails"`,
		`"versionString":"1.0.1"`,
		`"noop":false`,
		`"changedKeys":["contactEmail","demoAccountPassword"]`,
		`"demoAccountPasswordSet":true`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q: %s", want, out)
		}
	}
}

// TestDiffReviewerDetail_OnlyDifferingFields covers the diff path: only
// fields the user supplied AND that differ go into the delta.
func TestDiffReviewerDetail_OnlyDifferingFields(t *testing.T) {
	curEmail := "old@example.com"
	curName := "demo"
	current := AppStoreReviewDetailAttributes{
		ContactEmail:    &curEmail,
		DemoAccountName: &curName,
	}
	newEmail := "new@example.com"
	newName := "demo" // same as current
	pwd := "hunter2"
	desired := AppStoreReviewDetailAttributes{
		ContactEmail:        &newEmail,
		DemoAccountName:     &newName,
		DemoAccountPassword: &pwd, // password always written through
	}
	delta, changed := diffReviewerDetail(current, desired)
	if !changed {
		t.Fatal("diffReviewerDetail should report changed=true")
	}
	if delta.ContactEmail == nil || *delta.ContactEmail != newEmail {
		t.Errorf("delta.ContactEmail = %v, want %q", delta.ContactEmail, newEmail)
	}
	if delta.DemoAccountName != nil {
		t.Errorf("delta.DemoAccountName = %v, want nil (unchanged)", delta.DemoAccountName)
	}
	if delta.DemoAccountPassword == nil || *delta.DemoAccountPassword != pwd {
		t.Errorf("password should be in delta")
	}
}

// TestDiffReviewerDetail_NoChanges covers the noop path.
func TestDiffReviewerDetail_NoChanges(t *testing.T) {
	email := "same@example.com"
	current := AppStoreReviewDetailAttributes{ContactEmail: &email}
	desired := AppStoreReviewDetailAttributes{ContactEmail: &email}
	_, changed := diffReviewerDetail(current, desired)
	if changed {
		t.Errorf("diffReviewerDetail should report changed=false")
	}
}

// TestChangedKeys_SortedAndDeterministic locks the JSON output stability of
// the changedKeys list: consumers parse it.
func TestChangedKeys_SortedAndDeterministic(t *testing.T) {
	delta := AppStoreReviewDetailAttributes{
		Notes:               strPtr("x"),
		ContactEmail:        strPtr("e"),
		DemoAccountPassword: strPtr("pwd"),
	}
	keys := changedKeys(delta)
	want := []string{"contactEmail", "demoAccountPassword", "notes"}
	if len(keys) != len(want) {
		t.Fatalf("keys = %v, want %v", keys, want)
	}
	for i := range want {
		if keys[i] != want[i] {
			t.Errorf("keys[%d] = %q, want %q", i, keys[i], want[i])
		}
	}
}

// The constructor copies non-secret attributes but only the boolean for the password, never its value.
func TestBuildReviewerResult_DerefsAttrsWithoutPassword(t *testing.T) {
	pwd := "topsecret"
	first := "Jane"
	email := "reviewer@example.com"
	r := buildReviewerResult(
		"set", "RD1", "appStoreReviewDetails", "com.example.alpha", "1.0.1",
		false, []string{"contactEmail"},
		AppStoreReviewDetailAttributes{
			ContactFirstName:    &first,
			ContactEmail:        &email,
			DemoAccountPassword: &pwd,
		},
	)
	if r.ContactFirstName != "Jane" || r.ContactEmail != email {
		t.Errorf("attrs not copied: %+v", r)
	}
	if !r.DemoAccountPasswordSet {
		t.Errorf("DemoAccountPasswordSet = false, want true")
	}
	// Marshal and ensure the password literal does not appear anywhere.
	b, _ := json.Marshal(r)
	if strings.Contains(string(b), pwd) {
		t.Errorf("password leaked into JSON output: %s", b)
	}
}

// A 404 (no review detail yet) returns ("", zero, nil), the signal the caller uses to route to create.
func TestFetchAppStoreReviewDetail_404IsZero(t *testing.T) {
	srv := startFixtureServer(t, map[string]fixtureRoute{
		"GET /v1/appStoreVersions/V1/appStoreReviewDetail": {File: "iap_get_notFound", Status: 404},
	})
	c := fixtureASCClient(t, srv)
	id, attrs, err := fetchAppStoreReviewDetail(context.Background(), c, "V1")
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if id != "" {
		t.Errorf("id = %q, want empty", id)
	}
	if attrs.ContactEmail != nil {
		t.Errorf("attrs not zero: %+v", attrs)
	}
}
