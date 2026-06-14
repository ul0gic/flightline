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

//go:embed schema.json
var embeddedSchemaJSON []byte

// SchemaURL is the canonical $id of the embedded schema, used in the
// `# yaml-language-server: $schema=...` header of fetched state files.
const SchemaURL = "https://flightline.dev/schemas/v1alpha1/state.schema.json"

var (
	compiledSchemaOnce sync.Once
	compiledSchema     *jsonschema.Schema
	errCompiledSchema  error
)

// schema returns the lazy-compiled embedded schema; compilation is sticky on failure.
func schema() (*jsonschema.Schema, error) {
	compiledSchemaOnce.Do(func() {
		doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(embeddedSchemaJSON))
		if err != nil {
			errCompiledSchema = fmt.Errorf("config: parse embedded schema: %w", err)
			return
		}
		c := jsonschema.NewCompiler()
		if err := c.AddResource(SchemaURL, doc); err != nil {
			errCompiledSchema = fmt.Errorf("config: register embedded schema: %w", err)
			return
		}
		s, err := c.Compile(SchemaURL)
		if err != nil {
			errCompiledSchema = fmt.Errorf("config: compile embedded schema: %w", err)
			return
		}
		compiledSchema = s
	})
	return compiledSchema, errCompiledSchema
}

// Validate runs s through the embedded JSON Schema.
// Returns one Diagnostic per leaf-level failure; empty on a clean state.
func Validate(file string, s *State) []Diagnostic {
	if s == nil {
		return []Diagnostic{{File: file, Severity: SeverityError, Message: "state is nil"}}
	}

	sch, err := schema()
	if err != nil {
		return []Diagnostic{{File: file, Severity: SeverityError, Message: err.Error()}}
	}

	// Round-trip through JSON to get the validator's expected map shape.
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

// collectLeafErrors recurses into ve, emitting one Diagnostic per leaf cause only.
func collectLeafErrors(file string, ve *jsonschema.ValidationError, out *[]Diagnostic) {
	if len(ve.Causes) == 0 {
		*out = append(*out, validationToDiagnostic(file, ve))
		return
	}
	for _, c := range ve.Causes {
		collectLeafErrors(file, c, out)
	}
}

func validationToDiagnostic(file string, ve *jsonschema.ValidationError) Diagnostic {
	path := "/" + strings.Join(ve.InstanceLocation, "/")
	if path == "/" {
		path = ""
	}
	msg := ve.Error()
	// jsonschema prefixes Error() with a path+validate preamble; strip it so File+Path own location.
	if i := strings.Index(msg, ": "); i > 0 {
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
