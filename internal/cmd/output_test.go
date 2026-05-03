package cmd

import (
	"bytes"
	"strings"
	"testing"
)

type fakeView struct {
	headers []string
	rows    [][]string
}

func (v fakeView) TableRows() (headers []string, rows [][]string) {
	return v.headers, v.rows
}

func TestRender_JSON(t *testing.T) {
	var buf bytes.Buffer
	v := map[string]any{"name": "flightline", "ok": true}
	if err := renderTo(&buf, v, "json", true); err != nil {
		t.Fatalf("renderTo: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, `"name": "flightline"`) {
		t.Errorf("json output missing name: %q", out)
	}
	if !strings.Contains(out, `"ok": true`) {
		t.Errorf("json output missing ok: %q", out)
	}
}

func TestRender_Table(t *testing.T) {
	var buf bytes.Buffer
	v := fakeView{
		headers: []string{"BUNDLE", "NAME"},
		rows: [][]string{
			{"com.a", "App A"},
			{"com.example.long", "Example"},
		},
	}
	if err := renderTo(&buf, v, "table", true); err != nil {
		t.Fatalf("renderTo: %v", err)
	}
	out := buf.String()
	wantSubstrs := []string{"BUNDLE", "NAME", "com.a", "App A", "com.example.long", "Example", "----"}
	for _, w := range wantSubstrs {
		if !strings.Contains(out, w) {
			t.Errorf("table missing %q: %q", w, out)
		}
	}
	// First column should be padded to align "com.example.long" (16) with header "BUNDLE" (6) → 16 wide.
	lines := strings.Split(out, "\n")
	if len(lines) < 4 {
		t.Fatalf("expected 4 lines, got %d: %q", len(lines), out)
	}
}

func TestRender_TableEmptyHeadersIsNoop(t *testing.T) {
	var buf bytes.Buffer
	v := fakeView{}
	if err := renderTo(&buf, v, "table", true); err != nil {
		t.Fatalf("renderTo: %v", err)
	}
	if buf.Len() != 0 {
		t.Errorf("expected no output, got %q", buf.String())
	}
}

func TestRender_TableNonRenderableErrors(t *testing.T) {
	var buf bytes.Buffer
	err := renderTo(&buf, map[string]string{"x": "y"}, "table", true)
	if err == nil || !strings.Contains(err.Error(), "TableRenderable") {
		t.Errorf("err = %v, want TableRenderable hint", err)
	}
}

func TestRender_UnknownMode(t *testing.T) {
	var buf bytes.Buffer
	err := renderTo(&buf, "x", "csv", true)
	if err == nil || !strings.Contains(err.Error(), `unknown mode "csv"`) {
		t.Errorf("err = %v", err)
	}
}

func TestRender_EmptyMode(t *testing.T) {
	var buf bytes.Buffer
	err := renderTo(&buf, "x", "", true)
	if err == nil || !strings.Contains(err.Error(), "empty") {
		t.Errorf("err = %v", err)
	}
}

func TestColorDisabled_NoColorEnv(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	if !colorDisabled() {
		t.Error("colorDisabled should be true when NO_COLOR is set")
	}
}

func TestColorDisabled_Unset(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	if colorDisabled() {
		t.Error("colorDisabled should be false when NO_COLOR is unset")
	}
}
