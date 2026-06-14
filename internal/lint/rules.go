package lint

import "sync"

// rulesMu guards defaultRegistry; the lock matters most for tests that swap rule sets under t.Parallel.
var rulesMu sync.RWMutex

// defaultRegistry is the package-level registry every rule file appends to from its init().
var defaultRegistry []Rule

// Register adds a Rule to the package-level default registry. Re-registering an ID replaces the prior entry (test override).
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

// All returns a copy of the registry, safe to mutate without affecting future calls.
func All() []Rule {
	rulesMu.RLock()
	defer rulesMu.RUnlock()
	out := make([]Rule, len(defaultRegistry))
	copy(out, defaultRegistry)
	return out
}

// Filter returns registered rules whose Mode bitmask intersects mode; never mutates the registry.
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
