package lint

import (
	"fmt"
	"os"

	yaml "go.yaml.in/yaml/v3"
)

// strictRequiredNonzeroRule catches fields that pass `required` validation as empty strings.
// jsonschema only checks key presence; `Email string` always marshals as `"email":""`, so the YAML AST is walked instead. Offline-only.
type strictRequiredNonzeroRule struct{}

func init() { Register(strictRequiredNonzeroRule{}) }

func (strictRequiredNonzeroRule) ID() string         { return "strict.required-nonzero" }
func (strictRequiredNonzeroRule) Severity() Severity { return SeverityError }
func (strictRequiredNonzeroRule) Mode() Mode         { return ModeOffline }

func (r strictRequiredNonzeroRule) Check(ctx CheckContext) []Diagnostic {
	if ctx.SourcePath == "" {
		return nil
	}
	data, err := os.ReadFile(ctx.SourcePath) // #nosec G304 -- path is set by the lint command from its --file argument
	if err != nil {
		return nil
	}
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil
	}
	out := make([]Diagnostic, 0)
	r.scanTestFlightTesters(&root, &out)
	return out
}

// scanTestFlightTesters flags any tester under spec.testflight.groups.*.testers missing a non-empty email.
func (r strictRequiredNonzeroRule) scanTestFlightTesters(root *yaml.Node, out *[]Diagnostic) {
	groups := descendMap(root, "spec", "testflight", "groups")
	if groups == nil || groups.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i < len(groups.Content); i += 2 {
		groupName := groups.Content[i].Value
		groupNode := groups.Content[i+1]
		testers := childByKey(groupNode, "testers")
		if testers == nil || testers.Kind != yaml.SequenceNode {
			continue
		}
		for idx, t := range testers.Content {
			if t.Kind != yaml.MappingNode {
				continue
			}
			email := childByKey(t, "email")
			if email != nil && email.Value != "" {
				continue
			}
			line, col := t.Line, t.Column
			if email != nil {
				line, col = email.Line, email.Column
			}
			*out = append(*out, Diagnostic{
				RuleID:   r.ID(),
				Severity: SeverityError,
				Message: fmt.Sprintf(
					"line %d:%d: testflight group %q tester[%d] is missing a non-empty email",
					line, col, groupName, idx,
				),
				Path: fmt.Sprintf("/spec/testflight/groups/%s/testers/%d/email", groupName, idx),
				FixHint: "every tester row must have a non-empty `email` field. " +
					"Empty strings satisfy the schema's `required` (because the JSON key is present) but cannot be invited.",
				Reference: "QA-011 (resolved via this rule)",
			})
		}
	}
}

// descendMap walks a chain of mapping keys and returns the leaf node, or nil if any segment is absent.
func descendMap(root *yaml.Node, keys ...string) *yaml.Node {
	cur := root
	if cur != nil && cur.Kind == yaml.DocumentNode && len(cur.Content) > 0 {
		cur = cur.Content[0]
	}
	for _, k := range keys {
		cur = childByKey(cur, k)
		if cur == nil {
			return nil
		}
	}
	return cur
}

// childByKey returns the value node for a key in a mapping, or nil when absent or not a mapping.
func childByKey(n *yaml.Node, key string) *yaml.Node {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return nil
}
