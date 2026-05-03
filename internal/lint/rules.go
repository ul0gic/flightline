package lint

import "sync"

// rulesMu guards defaultRegistry. Rules typically self-register at init() so
// the mutex usage is bounded; the lock matters mostly for tests that swap
// rule sets under t.Parallel.
var rulesMu sync.RWMutex

// defaultRegistry is the package-level registry every rule file appends to
// from its init(). lint.All() returns a stable copy.
var defaultRegistry []Rule

// Register adds a Rule to the package-level default registry. Each rule
// file calls Register from its init(). Registration is idempotent on ID:
// re-registering the same ID replaces the prior entry rather than
// duplicating, so test code can override a rule without rewriting the
// registry list. Production code should never re-register the same ID.
func Register(rule Rule) {
	if rule == nil {
		return
	}
	id := rule.ID()
	rulesMu.Lock()
	defer rulesMu.Unlock()
	for i, existing := range defaultRegistry {
		if existing.ID() == id {
			defaultRegistry[i] = rule
			return
		}
	}
	defaultRegistry = append(defaultRegistry, rule)
}

// All returns a copy of the registry. The returned slice is safe to mutate
// (sort, filter) without affecting future calls.
func All() []Rule {
	rulesMu.RLock()
	defer rulesMu.RUnlock()
	out := make([]Rule, len(defaultRegistry))
	copy(out, defaultRegistry)
	return out
}

// Filter returns the subset of registered rules whose Mode bitmask intersects
// with mode. Pass ModeOffline to get the rules `lint` runs; pass ModeLive to
// get the rules `preflight` runs. Filter never mutates the registry.
func Filter(mode Mode) []Rule {
	all := All()
	out := make([]Rule, 0, len(all))
	for _, r := range all {
		if r.Mode()&mode != 0 {
			out = append(out, r)
		}
	}
	return out
}

// reset clears the registry. Test-only helper; not exported.
func reset() {
	rulesMu.Lock()
	defer rulesMu.Unlock()
	defaultRegistry = nil
}
