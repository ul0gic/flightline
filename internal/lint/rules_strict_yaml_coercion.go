package lint

import (
	"fmt"
	"os"
	"strings"

	yaml "go.yaml.in/yaml/v3"
)

// strictYAMLCoercionRule fires when a known bool field carries a YAML 1.1 coercion token (yes/no/on/off/y/n).
// yaml.v3 coerces these to bool even when quoted; the check inspects the raw AST Tag and Style. Offline-only.
type strictYAMLCoercionRule struct{}

func init() { Register(strictYAMLCoercionRule{}) }

func (strictYAMLCoercionRule) ID() string         { return "strict.yaml-coercion" }
func (strictYAMLCoercionRule) Severity() Severity { return SeverityError }
func (strictYAMLCoercionRule) Mode() Mode         { return ModeOffline }

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

// boolKeys is the set of state.yaml keys whose values must be true/false; yes/no/on/off are coercion footguns here.
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

// walkYAMLForCoercion descends the yaml.Node tree carrying parentKey so values correlate to known bool fields.
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
		// Not a known bool field; don't fire: could be a legitimate
		// string-typed field accidentally written as `yes`.
		return
	}
	// yaml.v3 coerces yes/no/on/off into bool even when quoted: flag both forms.
	style := "unquoted"
	switch n.Style {
	case yaml.DoubleQuotedStyle, yaml.SingleQuotedStyle:
		style = "quoted"
	}
	*out = append(*out, Diagnostic{
		RuleID:   ruleID,
		Severity: SeverityError,
		Message: fmt.Sprintf(
			"line %d:%d: bool field %q has %s YAML 1.1 token %q (yaml.v3 coerces yes/no/on/off to bool when decoding into *bool, even when quoted)",
			n.Line, n.Column, parentKey, style, n.Value,
		),
		Path: "/" + parentKey,
		FixHint: "use a real boolean: write `true` or `false`. " +
			"Quoting yes/no does not suppress the coercion in yaml.v3.",
		Reference: "QA-011 (resolved via this rule); yaml.v3 YAML 1.1 core schema",
	})
}

// isYAML11BoolToken returns true for YAML 1.1 boolean spellings (caller has already lowercased).
func isYAML11BoolToken(v string) bool {
	switch v {
	case "yes", "no", "on", "off", "y", "n":
		return true
	default:
		return false
	}
}
