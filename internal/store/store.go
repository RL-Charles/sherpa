// Package store persists Sherpa items as markdown files under .sherpa/.
package store

import (
	"cmp"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/RL-Charles/sherpa/internal/gitinfo"
	"github.com/RL-Charles/sherpa/internal/model"
)

const DirName = ".sherpa"

var ErrNotFound = errors.New("no .sherpa directory found (run `shp init`)")

type Config struct {
	Version  int      `yaml:"version"`
	Key      string   `yaml:"key"`
	Name     string   `yaml:"name"`
	Reviewer Reviewer `yaml:"reviewer,omitempty"`
}

// Reviewer is the agent that reviews agent submissions: a shell command that
// reads the review prompt on stdin, e.g. `claude -p`. With Auto it launches
// after every agent submit; otherwise on `shp review <id> --spawn`.
type Reviewer struct {
	Command string `yaml:"command,omitempty"`
	Auto    bool   `yaml:"auto,omitempty"`
}

type Store struct {
	Root     string // directory holding .sherpa
	Dir      string // absolute path to .sherpa
	CodeRoot string // checkout whose code is being worked on; git stats run here
	Config   Config
}

// Find locates the .sherpa directory for work happening in start:
//
//	$SHERPA_DIR                 explicit path to a .sherpa directory
//	inside a linked git worktree the main checkout's .sherpa, so every agent
//	                            worktree shares one live board
//	otherwise                   the nearest .sherpa walking up from start
//
// CodeRoot is always the checkout containing start, so diff stats measure
// the code actually being changed.
func Find(start string) (*Store, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return nil, err
	}
	top, main := gitinfo.Worktree(dir)
	withCode := func(s *Store, err error) (*Store, error) {
		if err == nil {
			s.CodeRoot = cmp.Or(top, s.Root)
		}
		return s, err
	}
	if env := os.Getenv("SHERPA_DIR"); env != "" {
		abs, err := filepath.Abs(env)
		if err != nil {
			return nil, err
		}
		if filepath.Base(abs) != DirName {
			abs = filepath.Join(abs, DirName)
		}
		return withCode(open(filepath.Dir(abs)))
	}
	if main != "" && main != top {
		if fi, err := os.Stat(filepath.Join(main, DirName)); err == nil && fi.IsDir() {
			return withCode(open(main))
		}
	}
	for {
		candidate := filepath.Join(dir, DirName)
		if fi, err := os.Stat(candidate); err == nil && fi.IsDir() {
			return withCode(open(dir))
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil, ErrNotFound
		}
		dir = parent
	}
}

func open(root string) (*Store, error) {
	s := &Store{Root: root, Dir: filepath.Join(root, DirName), CodeRoot: root}
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
	return s.Write(it)
}

// Write stores an item exactly as given, keeping its timestamps (imports).
func (s *Store) Write(it *model.Item) error {
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

// CheckoutKey names the checkout being worked in: "main" for the checkout
// holding .sherpa, otherwise the worktree's directory name plus a short hash
// of its path. The active ticket is tracked per checkout, so an agent's own
// shp commands and its tool hooks always agree, whatever their actor name.
func (s *Store) CheckoutKey() string {
	if s.CodeRoot == "" || s.CodeRoot == s.Root {
		return "main"
	}
	sum := sha1.Sum([]byte(s.CodeRoot))
	return keySafe.ReplaceAllString(filepath.Base(s.CodeRoot), "-") + "-" + hex.EncodeToString(sum[:3])
}

var keySafe = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func (s *Store) activePath() string {
	return filepath.Join(s.Dir, ".local", "active", s.CheckoutKey())
}

// Active returns the ticket claimed in this checkout, or "" if none.
func (s *Store) Active() (string, error) {
	data, err := os.ReadFile(s.activePath())
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	return strings.TrimSpace(string(data)), err
}

// SetActive records (or with "" clears) the ticket claimed in this checkout.
func (s *Store) SetActive(id string) error {
	if id == "" {
		if err := os.Remove(s.activePath()); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.activePath()), 0o755); err != nil {
		return err
	}
	return os.WriteFile(s.activePath(), []byte(id+"\n"), 0o644)
}
