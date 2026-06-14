package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// TableRenderable is implemented by values that render as a table; rows are
// cells in header order.
type TableRenderable interface {
	TableRows() (headers []string, rows [][]string)
}

// Render writes v to stdout in the requested mode; only data goes to stdout so it stays pipe-clean.
//
//	mode == "json"  → indented JSON of v
//	mode == "table" → ASCII table (v must implement TableRenderable)
func Render(v any, mode string) error {
	return renderTo(os.Stdout, v, mode, !isTTY(os.Stdout) || colorDisabled())
}

// renderTo is the testable inner Render; forcePlain strips ANSI/box-drawing
// chrome so piped output stays grep-clean.
func renderTo(w io.Writer, v any, mode string, forcePlain bool) error {
	switch mode {
	case "json":
		return writeJSON(w, v)
	case "table":
		return writeTable(w, v, forcePlain)
	case "":
		return errors.New("output: --output flag is empty (expected table | json)")
	default:
		return fmt.Errorf("output: unknown mode %q (expected table | json)", mode)
	}
}

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return fmt.Errorf("output: encode json: %w", err)
	}
	return nil
}

func writeTable(w io.Writer, v any, _ bool) error {
	t, ok := v.(TableRenderable)
	if !ok {
		return fmt.Errorf("output: value of type %T does not implement TableRenderable; use --output json", v)
	}
	headers, rows := t.TableRows()
	if len(headers) == 0 {
		// Empty result set: print nothing rather than bare table chrome.
		return nil
	}

	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = len(h)
	}
	for _, row := range rows {
		for i, cell := range row {
			if i >= len(widths) {
				continue
			}
			if l := len(cell); l > widths[i] {
				widths[i] = l
			}
		}
	}

	if err := writeRow(w, headers, widths); err != nil {
		return err
	}
	if err := writeRow(w, dashes(widths), widths); err != nil {
		return err
	}
	for _, row := range rows {
		if err := writeRow(w, row, widths); err != nil {
			return err
		}
	}
	return nil
}

func writeRow(w io.Writer, cells []string, widths []int) error {
	var sb strings.Builder
	for i, width := range widths {
		var cell string
		if i < len(cells) {
			cell = cells[i]
		}
		if i > 0 {
			sb.WriteString("  ")
		}
		sb.WriteString(cell)
		if pad := width - len(cell); pad > 0 && i < len(widths)-1 {
			sb.WriteString(strings.Repeat(" ", pad))
		}
	}
	sb.WriteByte('\n')
	_, err := io.WriteString(w, sb.String())
	if err != nil {
		return fmt.Errorf("output: write row: %w", err)
	}
	return nil
}

func dashes(widths []int) []string {
	out := make([]string, len(widths))
	for i, w := range widths {
		out[i] = strings.Repeat("-", w)
	}
	return out
}

// isTTY reports whether w is a terminal. Uses an os.Stat char-device check
// rather than golang.org/x/term to stay stdlib-only.
func isTTY(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// colorDisabled reports the NO_COLOR signal (any value, per no-color.org).
func colorDisabled() bool {
	return os.Getenv("NO_COLOR") != ""
}
