// Package store persists Sherpa items as markdown files under .sherpa/.
package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/RL-Charles/sherpa/internal/model"
)

const DirName = ".sherpa"

var ErrNotFound = errors.New("no .sherpa directory found (run `shp init`)")

type Config struct {
	Version int    `yaml:"version"`
	Key     string `yaml:"key"`
	Name    string `yaml:"name"`
}

type Store struct {
	Root   string // repository root (parent of .sherpa)
	Dir    string // absolute path to .sherpa
	Config Config
}

// Find walks up from start looking for a .sherpa directory.
func Find(start string) (*Store, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return nil, err
	}
	for {
		candidate := filepath.Join(dir, DirName)
		if fi, err := os.Stat(candidate); err == nil && fi.IsDir() {
			return open(dir)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil, ErrNotFound
		}
		dir = parent
	}
}

func open(root string) (*Store, error) {
	s := &Store{Root: root, Dir: filepath.Join(root, DirName)}
	data, err := os.ReadFile(filepath.Join(s.Dir, "config.yaml"))
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	if err := yaml.Unmarshal(data, &s.Config); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return s, nil
}

var keyPattern = regexp.MustCompile(`^[A-Z][A-Z0-9]{1,9}$`)

// Init creates a fresh .sherpa directory in root.
func Init(root, key, name string) (*Store, error) {
	key = strings.ToUpper(key)
	if !keyPattern.MatchString(key) {
		return nil, fmt.Errorf("project key %q must be 2-10 uppercase letters/digits", key)
	}
	dir := filepath.Join(root, DirName)
	if _, err := os.Stat(dir); err == nil {
		return nil, fmt.Errorf("%s already exists", dir)
	}
	for _, sub := range []string{"epics", "stories", "tickets", "sessions", ".local"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			return nil, err
		}
	}
	cfg := Config{Version: 1, Key: key, Name: name}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	files := map[string]string{
		"config.yaml":       string(data),
		".gitignore":        ".local/\n.index.db\n",
		"sessions/.gitkeep": "",
		"epics/.gitkeep":    "",
		"stories/.gitkeep":  "",
		"tickets/.gitkeep":  "",
	}
	for rel, content := range files {
		if err := os.WriteFile(filepath.Join(dir, rel), []byte(content), 0o644); err != nil {
			return nil, err
		}
	}
	return open(root)
}

func (s *Store) path(k model.Kind, id string) string {
	return filepath.Join(s.Dir, k.Dir(), id+".md")
}

// Get loads one item by ID (any accepted spelling).
func (s *Store) Get(id string) (*model.Item, error) {
	norm, k, _, err := model.ParseID(id)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(s.path(k, norm))
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("%s not found", norm)
	}
	if err != nil {
		return nil, err
	}
	it, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", norm, err)
	}
	return it, nil
}

// All loads every item, sorted epics → stories → tickets, then by ID.
func (s *Store) All() ([]*model.Item, error) {
	var items []*model.Item
	for _, k := range model.Kinds {
		files, err := filepath.Glob(filepath.Join(s.Dir, k.Dir(), "*.md"))
		if err != nil {
			return nil, err
		}
		for _, f := range files {
			data, err := os.ReadFile(f)
			if err != nil {
				return nil, err
			}
			it, err := Parse(data)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", filepath.Base(f), err)
			}
			items = append(items, it)
		}
	}
	rank := map[model.Kind]int{model.Epic: 0, model.Story: 1, model.Ticket: 2}
	sort.SliceStable(items, func(i, j int) bool {
		if rank[items[i].Kind] != rank[items[j].Kind] {
			return rank[items[i].Kind] < rank[items[j].Kind]
		}
		return items[i].ID < items[j].ID
	})
	return items, nil
}

// Save writes an item atomically and bumps its Updated time.
func (s *Store) Save(it *model.Item) error {
	it.Updated = time.Now().UTC().Truncate(time.Second)
	data, err := Render(it)
	if err != nil {
		return err
	}
	dst := s.path(it.Kind, it.ID)
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".tmp-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), dst)
}

// NextID returns the next unused ID for kind k.
func (s *Store) NextID(k model.Kind) (string, error) {
	files, err := filepath.Glob(filepath.Join(s.Dir, k.Dir(), "*.md"))
	if err != nil {
		return "", err
	}
	max := 0
	for _, f := range files {
		if _, _, n, err := model.ParseID(strings.TrimSuffix(filepath.Base(f), ".md")); err == nil && n > max {
			max = n
		}
	}
	return model.FormatID(k, max+1), nil
}

type NewItem struct {
	Kind        model.Kind
	Title       string
	Parent      string
	Description string
	Priority    string
	Labels      []string
}

// Create validates the parent link, allocates an ID and writes the item.
func (s *Store) Create(n NewItem) (*model.Item, error) {
	if strings.TrimSpace(n.Title) == "" {
		return nil, errors.New("title is required")
	}
	parent := ""
	if want, needs := n.Kind.ParentKind(); needs {
		if n.Parent == "" {
			return nil, fmt.Errorf("a %s needs a parent %s (--parent)", n.Kind, want)
		}
		p, err := s.Get(n.Parent)
		if err != nil {
			return nil, fmt.Errorf("parent: %w", err)
		}
		if p.Kind != want {
			return nil, fmt.Errorf("parent of a %s must be a %s, %s is a %s", n.Kind, want, p.ID, p.Kind)
		}
		parent = p.ID
	} else if n.Parent != "" {
		return nil, fmt.Errorf("an %s has no parent", n.Kind)
	}
	id, err := s.NextID(n.Kind)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC().Truncate(time.Second)
	it := &model.Item{
		ID:       id,
		Kind:     n.Kind,
		Title:    strings.TrimSpace(n.Title),
		Status:   model.Backlog,
		Parent:   parent,
		Priority: n.Priority,
		Labels:   n.Labels,
		Created:  now,
		Body:     model.DefaultBody(n.Kind, n.Description),
	}
	return it, s.Save(it)
}

// Children returns the direct children of id, in ID order.
func Children(items []*model.Item, id string) []*model.Item {
	var out []*model.Item
	for _, it := range items {
		if it.Parent == id {
			out = append(out, it)
		}
	}
	return out
}

// StripWorklog removes a legacy "## Worklog" section (heading through the
// next "## " heading or end of body). Worklogs now live in the journal.
func StripWorklog(body string) (string, bool) {
	lines := strings.Split(body, "\n")
	start := -1
	for i, l := range lines {
		if strings.TrimSpace(l) == "## Worklog" {
			start = i
			break
		}
	}
	if start < 0 {
		return body, false
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "## ") {
			end = i
			break
		}
	}
	out := append(lines[:start:start], lines[end:]...)
	return strings.TrimRight(strings.Join(out, "\n"), "\n") + "\n", true
}
