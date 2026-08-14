// Package jsonx extracts a JSON object from an LLM response that may wrap it in
// a markdown code fence or surround it with plain text (spec §6). Code-fence
// stripping alone is not enough: the observed failure mode is the model
// printing a plain-text answer BEFORE the JSON, so we also trim to the outer
// braces.
package jsonx

import "strings"

// Extract returns the best-effort JSON object substring from raw.
func Extract(raw string) string {
	s := StripFences(raw)
	s = trimBeforeFirstBrace(s)
	s = trimAfterLastBrace(s)
	return strings.TrimSpace(s)
}

// StripFences removes a surrounding ```...``` markdown code fence if present,
// including an optional language tag (e.g. ```json).
func StripFences(s string) string {
	t := strings.TrimSpace(s)
	if !strings.HasPrefix(t, "```") {
		return t
	}
	t = strings.TrimPrefix(t, "```")
	if i := strings.IndexByte(t, '\n'); i >= 0 {
		t = t[i+1:]
	}
	t = strings.TrimSuffix(strings.TrimSpace(t), "```")
	return strings.TrimSpace(t)
}

// trimBeforeFirstBrace drops any plain text before the first '{'.
func trimBeforeFirstBrace(s string) string {
	if i := strings.IndexByte(s, '{'); i > 0 {
		return s[i:]
	}
	return s
}

// trimAfterLastBrace drops any trailing text after the last '}'.
func trimAfterLastBrace(s string) string {
	if i := strings.LastIndexByte(s, '}'); i >= 0 && i < len(s)-1 {
		return s[:i+1]
	}
	return s
}
