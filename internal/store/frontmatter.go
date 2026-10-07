package store

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/RL-Charles/sherpa/internal/model"
)

const fence = "---"

// Parse reads a markdown file with a YAML frontmatter header into an Item.
func Parse(data []byte) (*model.Item, error) {
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	if !strings.HasPrefix(text, fence+"\n") {
		return nil, errors.New("missing frontmatter (file must start with ---)")
	}
	rest := text[len(fence)+1:]
	end := strings.Index(rest, "\n"+fence+"\n")
	var header, body string
	switch {
	case end >= 0:
		header, body = rest[:end], rest[end+len(fence)+2:]
	case strings.HasSuffix(rest, "\n"+fence):
		header = strings.TrimSuffix(rest, "\n"+fence)
	default:
		return nil, errors.New("unterminated frontmatter")
	}
	var it model.Item
	if err := yaml.Unmarshal([]byte(header), &it); err != nil {
		return nil, fmt.Errorf("frontmatter: %w", err)
	}
	it.Body = strings.TrimLeft(body, "\n")
	return &it, nil
}

// Render serialises an Item back to frontmatter + markdown.
func Render(it *model.Item) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString(fence + "\n")
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(it); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	buf.WriteString(fence + "\n\n")
	buf.WriteString(strings.TrimRight(it.Body, "\n"))
	buf.WriteString("\n")
	return buf.Bytes(), nil
}
