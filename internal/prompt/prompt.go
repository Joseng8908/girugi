// Package prompt loads prompt text from files on disk. Prompts are data, not
// code (CLAUDE.md rule 6): the planning owner edits prompts/*.md directly, so
// they are read at runtime rather than embedded.
package prompt

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"text/template"
)

// Loader reads prompt files from a base directory.
type Loader struct {
	dir string
}

func New(dir string) *Loader { return &Loader{dir: dir} }

// Load returns the raw contents of dir/name.md.
func (l *Loader) Load(name string) (string, error) {
	path := filepath.Join(l.dir, name+".md")
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("prompt: load %s: %w", name, err)
	}
	return string(b), nil
}

// Render loads dir/name.md and renders it as a text/template with data.
// Used by the report prompt to inject findings; personas ignore data.
func (l *Loader) Render(name string, data any) (string, error) {
	raw, err := l.Load(name)
	if err != nil {
		return "", err
	}
	t, err := template.New(name).Parse(raw)
	if err != nil {
		return "", fmt.Errorf("prompt: parse %s: %w", name, err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("prompt: render %s: %w", name, err)
	}
	return buf.String(), nil
}
