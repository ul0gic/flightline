// schema.go — embedded JSON Schema validation.
//
// LoadState (load.go) handles structural decode; this file handles the
// cross-field, format, and pattern rules expressed in
// schemas/flightline.schema.json that Go's type system can't capture.
//
// The schema is embedded into the binary so `fline plan` / `fline apply`
// work without any sidecar file resolution. Callers receive a
// flat []Diagnostic — one per leaf-level validation failure with a
// JSON-Pointer Path so editors and humans can both jump to the right
// field.

package config

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// schema.json is a build-time copy of schemas/flightline.schema.json kept
// in this package for `//go:embed` (which forbids `..` traversal). The
// Makefile target `sync-schema` keeps the two in lock-step; a CI guard
// rejects diffs.
//
//go:embed schema.json
var embeddedSchemaJSON []byte

// SchemaURL is the canonical $id of the embedded schema. Used when
// emitting `# yaml-language-server: $schema=...` headers in fetched
// state files.
const SchemaURL = "https://flightline.dev/schemas/v1alpha1/state.schema.json"

var (
	compiledSchemaOnce sync.Once
	compiledSchema     *jsonschema.Schema
	compiledSchemaErr  error
)

// schema returns the lazy-compiled embedded schema. Compilation runs
// once per process; failures (which would indicate a corrupted build)
// are sticky.
func schema() (*jsonschema.Schema, error) {
	compiledSchemaOnce.Do(func() {
		doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(embeddedSchemaJSON))
		if err != nil {
			compiledSchemaErr = fmt.Errorf("config: parse embedded schema: %w", err)
			return
		}
		c := jsonschema.NewCompiler()
		if err := c.AddResource(SchemaURL, doc); err != nil {
			compiledSchemaErr = fmt.Errorf("config: register embedded schema: %w", err)
			return
		}
		s, err := c.Compile(SchemaURL)
		if err != nil {
			compiledSchemaErr = fmt.Errorf("config: compile embedded schema: %w", err)
			return
		}
		compiledSchema = s
	})
	return compiledSchema, compiledSchemaErr
}

// Validate runs s through the embedded JSON Schema and returns one
// Diagnostic per leaf-level failure. The returned slice is empty when
// the state matches the schema.
//
// Diagnostics carry a JSON-Pointer Path (e.g. /spec/iap/products/com.x.y/type)
// but no Line/Column — those come from the YAML loader. Callers wanting
// position-anchored schema errors should keep the YAML loader's Line/Col
// from LoadState and merge by path.
//
// file is the source path used in each Diagnostic.File so error
// renderers can show the originating file.
func Validate(file string, s *State) []Diagnostic {
	if s == nil {
		return []Diagnostic{{File: file, Severity: SeverityError, Message: "state is nil"}}
	}

	sch, err := schema()
	if err != nil {
		return []Diagnostic{{File: file, Severity: SeverityError, Message: err.Error()}}
	}

	// Round-trip through JSON to get the validator's expected shape
	// (map[string]any with json.Number-free numerics). yaml.v3 already
	// produced Go scalars, so this is a clean marshal/unmarshal pair.
	buf, err := json.Marshal(s)
	if err != nil {
		return []Diagnostic{{File: file, Severity: SeverityError, Message: fmt.Sprintf("marshal state: %v", err)}}
	}
	var instance any
	if err := json.Unmarshal(buf, &instance); err != nil {
		return []Diagnostic{{File: file, Severity: SeverityError, Message: fmt.Sprintf("re-decode state: %v", err)}}
	}

	verr := sch.Validate(instance)
	if verr == nil {
		return nil
	}

	var ve *jsonschema.ValidationError
	if !errors.As(verr, &ve) {
		return []Diagnostic{{File: file, Severity: SeverityError, Message: verr.Error()}}
	}

	var diags []Diagnostic
	collectLeafErrors(file, ve, &diags)
	return diags
}

// collectLeafErrors walks the ValidationError tree and emits one
// Diagnostic per leaf cause. Internal nodes (which carry meta-rules
// like "oneOf failed") get folded so users see actionable field-level
// problems, not the schema mechanism behind them.
func collectLeafErrors(file string, ve *jsonschema.ValidationError, out *[]Diagnostic) {
	if len(ve.Causes) == 0 {
		*out = append(*out, validationToDiagnostic(file, ve))
		return
	}
	for _, c := range ve.Causes {
		collectLeafErrors(file, c, out)
	}
}

// validationToDiagnostic projects a single ValidationError into a
// Diagnostic with a JSON-Pointer-style Path.
func validationToDiagnostic(file string, ve *jsonschema.ValidationError) Diagnostic {
	path := "/" + strings.Join(ve.InstanceLocation, "/")
	if path == "/" {
		path = ""
	}
	msg := ve.Error()
	// jsonschema's default Error() prefixes with "jsonschema: '<path>' does not validate ..."
	// which is verbose. Trim the path prefix so our File+Path columns own location.
	if i := strings.Index(msg, ": "); i > 0 {
		// keep only the trailing reason after the second ": " when present
		if j := strings.Index(msg[i+2:], ": "); j > 0 {
			msg = strings.TrimSpace(msg[i+2+j+2:])
		}
	}
	return Diagnostic{
		File:     file,
		Path:     path,
		Severity: SeverityError,
		Message:  msg,
	}
}
