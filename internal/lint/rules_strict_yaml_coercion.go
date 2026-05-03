package lint

import (
	"fmt"
	"os"
	"strings"

	yaml "go.yaml.in/yaml/v3"
)

// strictYAMLCoercionRule walks the source state YAML tree looking for raw
// boolean-coercion footguns: `yes`, `no`, `on`, `off`, `y`, `n` (any case)
// at any node. yaml.v3 follows YAML 1.1 core schema and silently coerces
// these tokens to bool — even when written like `gambling: "yes"` in some
// edge cases (string survives but the *intent* mismatch is a footgun).
//
// The check fires only when the surrounding context implies a boolean
// field. We don't have type info from the parsed *State here; instead we
// look at the raw bytes via yaml.Node and inspect Tag + Style:
//   - Tag == "!!bool" and Style is unquoted (Plain): the user wrote `yes`,
//     yaml.v3 coerced it to bool. This is the QA-011 footgun.
//   - Tag == "!!str" and Style is quoted: the user wrote `"yes"`, intent
//     preserved as string — only fire if the parent key is a known bool
//     field (we keep this conservative).
//
// Offline-only.
type strictYAMLCoercionRule struct{}

func init() { Register(strictYAMLCoercionRule{}) }

func (strictYAMLCoercionRule) ID() string         { return "strict.yaml-coercion" }
func (strictYAMLCoercionRule) Severity() Severity { return SeverityError }
func (strictYAMLCoercionRule) Mode() Mode         { return ModeOffline }

// strict.yaml-coercion reads CheckContext.SourcePath — the absolute path of
// the YAML the user is linting — and walks the raw AST. When SourcePath is
// empty (e.g. preflight against fetched live state) the rule no-ops: no
// source means nothing to scan.

func (r strictYAMLCoercionRule) Check(ctx CheckContext) []Diagnostic {
	if ctx.SourcePath == "" {
		return nil
	}
	data, err := os.ReadFile(ctx.SourcePath) // #nosec G304 -- path is set by the lint command from its --file argument
	if err != nil {
		return nil
	}
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil // load.go has already surfaced the parse error
	}
	out := make([]Diagnostic, 0)
	walkYAMLForCoercion(&root, "", &out, r.ID())
	return out
}

// boolKeys is the conservative set of state.yaml keys whose values are
// expected to be booleans. Quoted yes/no on these keys is still a footgun
// (the user thinks they're answering Apple's question with a string).
var boolKeys = map[string]struct{}{
	"usesNonExemptEncryption":                   {},
	"availableOnFrenchStore":                    {},
	"containsProprietaryCryptography":           {},
	"containsThirdPartyCryptography":            {},
	"usesEncryption":                            {},
	"exempt":                                    {},
	"prolongedGraphicSadisticRealisticViolence": {},
	"gambling":                                  {},
	"unrestrictedWebAccess":                     {},
	"seventeenPlus":                             {},
	"familySharable":                            {},
	"contentHosting":                            {},
	"downloadable":                              {},
	"isInternal":                                {},
	"publicLink":                                {},
	"visible":                                   {},
}

// walkYAMLForCoercion descends the yaml.Node tree carrying the parent key
// (when on a mapping value) so we can correlate the value back to a known
// bool field.
func walkYAMLForCoercion(n *yaml.Node, parentKey string, out *[]Diagnostic, ruleID string) {
	if n == nil {
		return
	}
	switch n.Kind {
	case yaml.DocumentNode:
		for _, c := range n.Content {
			walkYAMLForCoercion(c, parentKey, out, ruleID)
		}
	case yaml.MappingNode:
		for i := 0; i < len(n.Content); i += 2 {
			k, v := n.Content[i], n.Content[i+1]
			walkYAMLForCoercion(v, k.Value, out, ruleID)
		}
	case yaml.SequenceNode:
		for _, c := range n.Content {
			walkYAMLForCoercion(c, parentKey, out, ruleID)
		}
	case yaml.ScalarNode:
		checkScalar(n, parentKey, out, ruleID)
	}
}

func checkScalar(n *yaml.Node, parentKey string, out *[]Diagnostic, ruleID string) {
	if n == nil || n.Value == "" {
		return
	}
	v := strings.ToLower(n.Value)
	if !isYAML11BoolToken(v) {
		return
	}
	if _, isBoolField := boolKeys[parentKey]; !isBoolField {
		// Not a known bool field; don't fire — could be a legitimate
		// string-typed field accidentally written as `yes`.
		return
	}
	// At this point: a known bool field carries a yes/no/on/off/y/n token.
	// yaml.v3 coerces yes/no into bool when decoding into *bool — quoting
	// does NOT suppress this. Flag both quoted and unquoted forms with a
	// note about which the user wrote.
	style := "unquoted"
	switch n.Style {
	case yaml.DoubleQuotedStyle, yaml.SingleQuotedStyle:
		style = "quoted"
	}
	*out = append(*out, Diagnostic{
		RuleID:   ruleID,
		Severity: SeverityError,
		Message: fmt.Sprintf(
			"line %d:%d — bool field %q has %s YAML 1.1 token %q (yaml.v3 coerces yes/no/on/off to bool when decoding into *bool, even when quoted)",
			n.Line, n.Column, parentKey, style, n.Value,
		),
		Path: "/" + parentKey,
		FixHint: "use a real boolean: write `true` or `false`. " +
			"Quoting yes/no does not suppress the coercion in yaml.v3.",
		Reference: "QA-011 (resolved via this rule); yaml.v3 YAML 1.1 core schema",
	})
}

// isYAML11BoolToken returns true for the YAML 1.1 boolean spellings.
// We check lowercase form; the caller has already lowercased.
func isYAML11BoolToken(v string) bool {
	switch v {
	case "yes", "no", "on", "off", "y", "n":
		return true
	default:
		return false
	}
}
