package aiagent

import (
	"embed"
	"fmt"
	"strings"
)

//go:embed prompts/*.md
var promptFS embed.FS

// SystemPrompt returns the named system prompt with {{SCHEMA}} substituted.
// Valid kinds: "oneshot", "chat", "advisor".
func SystemPrompt(kind string) (string, error) {
	name := "prompts/" + kind + ".md"
	data, err := promptFS.ReadFile(name)
	if err != nil {
		return "", fmt.Errorf("load prompt %s: %w", kind, err)
	}
	return strings.ReplaceAll(string(data), "{{SCHEMA}}", SchemaJSON()), nil
}
