// Package policy decides which agent work still needs a human. By default a
// ticket closes on a passing agent review; .sherpa/policy.yaml names the
// exceptions and what happens around a close:
//
//	human_review:                  # a ticket matching any rule waits for a human
//	  - name: migrations
//	    paths: [migrations/**]     # its submit or code map touches one of these
//	  - name: flagged
//	    labels: [security]         # it has any of these labels
//	  - name: large
//	    min_lines: 400             # its submit changed at least this many lines
//	commit: auto                   # auto | off: commit its files when an agent closes it
//	sample: 0.1                    # share of auto-closed tickets flagged for a human audit
//	verify: go test ./...          # what the reviewer runs before passing work
//
// Every condition a rule gives must hold; use several rules for "or".
// Stories and epics always close by a human, and so does a ticket with an
// unresolved deliverable or check.
package policy

import (
	"errors"
	"fmt"
	"hash/fnv"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/RL-Charles/sherpa/internal/model"
)

const File = "policy.yaml"

// Commit modes.
const (
	CommitOff  = "off"
	CommitAuto = "auto"
)

type Rule struct {
	Name     string   `yaml:"name"`
	Labels   []string `yaml:"labels,omitempty"`
	Under    []string `yaml:"under,omitempty"`
	Paths    []string `yaml:"paths,omitempty"`
	MinLines int      `yaml:"min_lines,omitempty"`
}

type Policy struct {
	HumanReview []Rule  `yaml:"human_review,omitempty"`
	Commit      string  `yaml:"commit,omitempty"`
	Sample      float64 `yaml:"sample,omitempty"`
	Verify      string  `yaml:"verify,omitempty"`

	// Found reports whether the project has a policy file at all.
	Found bool `yaml:"-"`
	// AutoDone is the retired allowlist; Load refuses a file that still has it.
	AutoDone []yaml.Node `yaml:"auto_done,omitempty" json:"-"`
}

// Change is what a ticket's last submit touched.
type Change struct {
	Lines int
	Files []string
}

// Load reads the policy from a .sherpa directory; a missing file is the
// default policy: no human_review rules, no commits, no sampling.
func Load(sherpaDir string) (Policy, error) {
	var p Policy
	data, err := os.ReadFile(filepath.Join(sherpaDir, File))
	if errors.Is(err, os.ErrNotExist) {
		return p, nil
	}
	if err != nil {
		return p, err
	}
	if err := yaml.Unmarshal(data, &p); err != nil {
		return p, fmt.Errorf("parse %s: %w", File, err)
	}
	p.Found = true
	if len(p.AutoDone) > 0 {
		return p, fmt.Errorf("%s: auto_done is retired: tickets now close on a passing agent review unless a human_review rule matches; replace auto_done with human_review rules (shp policy init --force drafts them)", File)
	}
	switch p.Commit {
	case "":
		p.Commit = CommitOff
	case CommitOff, CommitAuto:
	default:
		return p, fmt.Errorf("%s: commit must be auto or off, not %q", File, p.Commit)
	}
	if p.Sample < 0 || p.Sample > 1 {
		return p, fmt.Errorf("%s: sample must be between 0 and 1, not %v", File, p.Sample)
	}
	for i, r := range p.HumanReview {
		if r.Name == "" {
			return p, fmt.Errorf("%s: human_review rule %d needs a name", File, i+1)
		}
		if len(r.Labels) == 0 && len(r.Under) == 0 && len(r.Paths) == 0 && r.MinLines == 0 {
			return p, fmt.Errorf("%s: rule %q needs labels, under, paths or min_lines", File, r.Name)
		}
		for j, id := range r.Under {
			norm, _, _, err := model.ParseID(id)
			if err != nil {
				return p, fmt.Errorf("%s: rule %q: %w", File, r.Name, err)
			}
			p.HumanReview[i].Under[j] = norm
		}
		for _, g := range r.Paths {
			if _, err := path.Match(strings.ReplaceAll(g, "**", "*"), ""); err != nil {
				return p, fmt.Errorf("%s: rule %q: bad path %q: %w", File, r.Name, g, err)
			}
		}
	}
	return p, nil
}

// Human returns why a human must close it, or "" when a passing agent
// review may close it.
func (p Policy) Human(it *model.Item, all []*model.Item, ch Change) string {
	if it.Kind != model.Ticket {
		return "stories and epics close by a human"
	}
	d, t := it.DeliverableCounts()
	switch {
	case t == 0:
		return "no deliverables"
	case d < t:
		return fmt.Sprintf("deliverables %d/%d unresolved", d, t)
	}
	if r, t := it.CheckCounts(); t == 0 {
		return "no checks"
	} else if r < t {
		if len(it.OpenManual()) > 0 {
			return "manual checks wait for a human"
		}
		return fmt.Sprintf("checks %d/%d unresolved", r, t)
	}
	if name, why := p.Rule(it, all, ch); name != "" {
		return "rule " + name + ": " + why
	}
	return ""
}

// Rule returns the first human_review rule that matches it and what matched.
func (p Policy) Rule(it *model.Item, all []*model.Item, ch Change) (name, why string) {
	files := slices.Clone(ch.Files)
	for _, c := range it.Code {
		if !slices.Contains(files, c.Path) {
			files = append(files, c.Path)
		}
	}
	for _, r := range p.HumanReview {
		if why, ok := r.matches(it, all, ch.Lines, files); ok {
			return r.Name, why
		}
	}
	return "", ""
}

func (r Rule) matches(it *model.Item, all []*model.Item, lines int, files []string) (string, bool) {
	var why []string
	if len(r.Labels) > 0 {
		i := slices.IndexFunc(it.Labels, func(l string) bool { return slices.Contains(r.Labels, l) })
		if i < 0 {
			return "", false
		}
		why = append(why, "label "+it.Labels[i])
	}
	if len(r.Under) > 0 {
		anc := under(it, all, r.Under)
		if anc == "" {
			return "", false
		}
		why = append(why, "under "+anc)
	}
	if len(r.Paths) > 0 {
		hit := ""
		for _, f := range files {
			if slices.ContainsFunc(r.Paths, func(g string) bool { return Match(g, f) }) {
				hit = f
				break
			}
		}
		if hit == "" {
			return "", false
		}
		why = append(why, "touches "+hit)
	}
	if r.MinLines > 0 {
		if lines < r.MinLines {
			return "", false
		}
		why = append(why, fmt.Sprintf("%d lines changed", lines))
	}
	return strings.Join(why, ", "), true
}

// under returns the first ancestor of it that is in ids, or "".
func under(it *model.Item, all []*model.Item, ids []string) string {
	byID := map[string]*model.Item{}
	for _, x := range all {
		byID[x.ID] = x
	}
	seen := map[string]bool{}
	for p := it.Parent; p != "" && !seen[p]; {
		if slices.Contains(ids, p) {
			return p
		}
		seen[p] = true
		x := byID[p]
		if x == nil {
			return ""
		}
		p = x.Parent
	}
	return ""
}

// Match reports whether a slash-separated file path matches a glob. `**`
// spans any number of directories; a pattern without a slash matches the
// file's base name anywhere, as in .gitignore.
func Match(glob, file string) bool {
	file = strings.TrimPrefix(filepath.ToSlash(file), "./")
	glob = strings.TrimPrefix(glob, "./")
	if !strings.Contains(glob, "/") {
		ok, _ := path.Match(glob, path.Base(file))
		return ok
	}
	return matchParts(strings.Split(glob, "/"), strings.Split(file, "/"))
}

func matchParts(glob, file []string) bool {
	for len(glob) > 0 {
		if glob[0] == "**" {
			for i := 0; i <= len(file); i++ {
				if matchParts(glob[1:], file[i:]) {
					return true
				}
			}
			return false
		}
		if len(file) == 0 {
			return false
		}
		if ok, _ := path.Match(glob[0], file[0]); !ok {
			return false
		}
		glob, file = glob[1:], file[1:]
	}
	return len(file) == 0
}

// Sampled reports whether an auto-closed ticket is picked for a human audit.
// The pick is stable per ID, so re-running a close never changes it.
func (p Policy) Sampled(id string) bool {
	if p.Sample <= 0 {
		return false
	}
	h := fnv.New32a()
	h.Write([]byte(id))
	return float64(h.Sum32()%10000) < p.Sample*10000
}
