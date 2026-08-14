// Package domain holds the scenario/persona/disclose contract shared with the
// frontend (API.md). The disclose whitelist MUST be enforced in code, not by
// trusting the prompt (CLAUDE.md rule 8).
package domain

import "slices"

// Scenarios.
const (
	ScenarioCMF       = "cmf_outsourcing"
	ScenarioPrototype = "prototype_revision"
)

// Personas.
const (
	PersonaSenior      = "senior"
	PersonaEngineering = "engineering"
	PersonaPurchasing  = "purchasing"
	PersonaDesign      = "design"
)

// ScenarioPersonas lists which personas each scenario exposes.
var ScenarioPersonas = map[string][]string{
	ScenarioCMF:       {PersonaSenior, PersonaEngineering, PersonaPurchasing, PersonaDesign},
	ScenarioPrototype: {PersonaSenior, PersonaEngineering, PersonaDesign},
}

// DiscloseKeys is the [scenario][persona] → allowed disclose keys whitelist.
// The LLM sometimes invents keys; anything outside this set is stripped so the
// frontend's document-unlock mapping never receives an unknown key.
var DiscloseKeys = map[string]map[string][]string{
	ScenarioCMF: {
		PersonaSenior:      {"spec_format", "limit_sample", "vendor_criteria"},
		PersonaEngineering: {"inhouse_capability", "sheet_lead_time"},
		PersonaPurchasing:  {"budget_limit", "cost_impact", "part_cost_share"},
		PersonaDesign:      {"wood_material_intent", "concept_consistency_criteria", "cmf_alternative_range"},
	},
	ScenarioPrototype: {
		PersonaEngineering: {"molding_cost_constraint", "part_consolidation_criteria", "undercut_constraint", "min_thickness"},
		PersonaSenior:      {"belt_line_intent", "revision_guidance_criteria"},
		PersonaDesign:      {"user_requirement_keywords", "concept_direction"},
	},
}

// ValidScenario reports whether s is a known scenario.
func ValidScenario(s string) bool {
	_, ok := ScenarioPersonas[s]
	return ok
}

// ValidPersona reports whether persona belongs to scenario.
func ValidPersona(scenario, persona string) bool {
	return slices.Contains(ScenarioPersonas[scenario], persona)
}

// FilterDisclose keeps only keys allowed for scenario+persona, preserving order
// and dropping duplicates. Returns nil when nothing survives.
func FilterDisclose(scenario, persona string, keys []string) []string {
	allowed := DiscloseKeys[scenario][persona]
	if len(keys) == 0 || len(allowed) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(allowed))
	for _, k := range allowed {
		set[k] = struct{}{}
	}
	seen := make(map[string]struct{}, len(keys))
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		if _, ok := set[k]; !ok {
			continue
		}
		if _, dup := seen[k]; dup {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, k)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
