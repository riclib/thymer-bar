package bit

import (
	"strings"

	"gopkg.in/yaml.v3"
)

// ParseMarkdown splits markdown with YAML frontmatter into components.
// Input format:
//
//	---
//	title: My Note
//	tags: [a, b]
//	---
//	The markdown content...
//
// Returns frontmatter map, content string, and any parse error.
func ParseMarkdown(data string) (map[string]any, string, error) {
	// Check for frontmatter delimiter
	if !strings.HasPrefix(data, "---\n") && !strings.HasPrefix(data, "---\r\n") {
		// No frontmatter, return content only
		return nil, data, nil
	}

	// Find closing delimiter
	rest := data[4:] // Skip opening "---\n"
	idx := strings.Index(rest, "\n---")
	if idx == -1 {
		// No closing delimiter, treat as content only
		return nil, data, nil
	}

	fmYAML := rest[:idx]
	content := strings.TrimPrefix(rest[idx+4:], "\n")

	// Parse YAML frontmatter
	var fm map[string]any
	if err := yaml.Unmarshal([]byte(fmYAML), &fm); err != nil {
		return nil, "", err
	}

	return fm, content, nil
}

// ToMarkdown serializes a Bit to markdown with YAML frontmatter.
// Output format:
//
//	---
//	title: My Note
//	tags: [a, b]
//	---
//
//	The markdown content...
func (b *Bit) ToMarkdown() (string, error) {
	if len(b.Frontmatter) == 0 && b.Content == "" {
		return "", nil
	}

	var sb strings.Builder

	// Write frontmatter if present
	if len(b.Frontmatter) > 0 {
		fmBytes, err := yaml.Marshal(b.Frontmatter)
		if err != nil {
			return "", err
		}

		sb.WriteString("---\n")
		sb.Write(fmBytes)
		sb.WriteString("---\n\n")
	}

	// Write content
	sb.WriteString(b.Content)

	return sb.String(), nil
}

// FromMarkdown creates a Bit from markdown with optional frontmatter.
func FromMarkdown(masterURI, masterSystem, markdown string) (*Bit, error) {
	fm, content, err := ParseMarkdown(markdown)
	if err != nil {
		return nil, err
	}

	b := New(masterURI, masterSystem)
	if fm != nil {
		b.Frontmatter = fm
	}
	b.Content = content
	b.UpdateHash()

	return b, nil
}
