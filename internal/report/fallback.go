package report

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Fallback holds the fixed sentences used when the LLM is unavailable or its
// output cannot be parsed. The report is the last screen and must never be
// empty (spec §5), so this is loaded once at startup from prompts/fallback.json.
type Fallback struct {
	// Sentences maps a finding code → a canned sentence (session 2).
	Sentences map[string]string
	// S1Default is the whole session-1 narrative fallback (_s1_default).
	S1Default resS1
}

// LoadFallback reads prompts/fallback.json. On error it returns an empty (but
// non-nil) Fallback so the server can still boot; the caller logs the error.
func LoadFallback(dir string) (*Fallback, error) {
	fb := &Fallback{Sentences: map[string]string{}}
	b, err := os.ReadFile(filepath.Join(dir, "fallback.json"))
	if err != nil {
		return fb, fmt.Errorf("report: load fallback.json: %w", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return fb, fmt.Errorf("report: parse fallback.json: %w", err)
	}
	for k, v := range raw {
		if k == "_s1_default" {
			_ = json.Unmarshal(v, &fb.S1Default)
			continue
		}
		var s string
		if json.Unmarshal(v, &s) == nil {
			fb.Sentences[k] = s
		}
	}
	return fb, nil
}

// forS2 builds the session-2 response from finding codes, honoring the array
// caps (spec §5: 2/2/3).
func (f *Fallback) forS2(req request) resS2 {
	return resS2{
		Strengths: f.sentencesFor(req.StrengthsFindings, 2),
		Cautions:  f.sentencesFor(req.CautionsFindings, 2),
		Missed:    f.sentencesFor(req.MissedFindings, 3),
	}
}

// sentencesFor maps finding codes to canned sentences, up to limit. Unknown
// codes are skipped.
func (f *Fallback) sentencesFor(fs []Finding, limit int) []string {
	out := []string{}
	for _, fd := range fs {
		s, ok := f.Sentences[fd.Code]
		if !ok {
			continue
		}
		out = append(out, s)
		if len(out) >= limit {
			break
		}
	}
	return out
}
