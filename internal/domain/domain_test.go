package domain

import (
	"slices"
	"testing"
)

// TestFilterDiscloseSeniorKeys guards the disclose whitelist for the CMF senior
// persona. Whenever a prompt adds a disclose key it must be added here too, or
// FilterDisclose silently strips it (issue: PR #17/#18 senior keys).
func TestFilterDiscloseSeniorKeys(t *testing.T) {
	in := []string{"product_concept", "design_direction", "cmf_spec", "made_up"}
	got := FilterDisclose(ScenarioCMF, PersonaSenior, in)
	want := []string{"product_concept", "design_direction", "cmf_spec"}
	if !slices.Equal(got, want) {
		t.Fatalf("FilterDisclose = %v, want %v (made_up must be stripped, the 3 real keys must pass)", got, want)
	}
}

func TestFilterDiscloseCrossPersonaIsolation(t *testing.T) {
	// engineering key must not pass through the senior whitelist.
	if got := FilterDisclose(ScenarioCMF, PersonaSenior, []string{"inhouse_capability"}); got != nil {
		t.Fatalf("expected nil (cross-persona key stripped), got %v", got)
	}
	// senior keys must not pass through the prototype scenario.
	if got := FilterDisclose(ScenarioPrototype, PersonaSenior, []string{"spec_format"}); got != nil {
		t.Fatalf("expected nil (cross-scenario key stripped), got %v", got)
	}
}
