// Package policy decides which agent work a passing agent review may close
// without a human. It reads .sherpa/policy.yaml:
//
//	auto_done:
//	  - name: docs
//	    labels: [docs, tests]   # the item has any of these labels
//	  - name: tooling
//	    under: [S-008]          # the item sits under any of these stories/epics
//	    max_lines: 200          # and its submit changed at most this many lines
//
// Every condition a rule gives must hold. With no file nothing auto-closes,
// and work with an unresolved check, manual or not, always goes to a human.
package policy

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"gopkg.in/yaml.v3"

	"github.com/RL-Charles/sherpa/internal/model"
)

const File = "policy.yaml"

type Rule struct {
	Name     string   `yaml:"name"`
	Labels   []string `yaml:"labels,omitempty"`
	Under    []string `yaml:"under,omitempty"`
	MaxLines int      `yaml:"max_lines,omitempty"`
}

type Policy struct {
	AutoDone []Rule `yaml:"auto_done"`
}

// Load reads the policy from a .sherpa directory; a missing file is the
// empty policy.
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
	for i, r := range p.AutoDone {
		if r.Name == "" {
			return p, fmt.Errorf("%s: auto_done rule %d needs a name", File, i+1)
		}
		if len(r.Labels) == 0 && len(r.Under) == 0 {
			return p, fmt.Errorf("%s: rule %q must limit by labels or under; a rule matching everything is not allowed", File, r.Name)
		}
		for j, id := range r.Under {
			norm, _, _, err := model.ParseID(id)
			if err != nil {
				return p, fmt.Errorf("%s: rule %q: %w", File, r.Name, err)
			}
			p.AutoDone[i].Under[j] = norm
		}
	}
	return p, nil
}

// Closes returns the rule that lets a passing review close it, or "" with
// the reason a human still has to.
func (p Policy) Closes(it *model.Item, all []*model.Item, changedLines int) (rule, why string) {
	if len(p.AutoDone) == 0 {
		return "", "no auto_done rules in " + File
	}
	if r, t := it.CheckCounts(); r < t {
		if len(it.OpenManual()) > 0 {
			return "", "manual checks wait for a human"
		}
		return "", fmt.Sprintf("checks %d/%d unresolved", r, t)
	}
	for _, r := range p.AutoDone {
		if r.matches(it, all, changedLines) {
			return r.Name, ""
		}
	}
	return "", "no auto_done rule matches"
}

func (r Rule) matches(it *model.Item, all []*model.Item, changed int) bool {
	if len(r.Labels) > 0 && !slices.ContainsFunc(it.Labels, func(l string) bool { return slices.Contains(r.Labels, l) }) {
		return false
	}
	if len(r.Under) > 0 && !under(it, all, r.Under) {
		return false
	}
	return r.MaxLines == 0 || changed <= r.MaxLines
}

// under reports whether any ancestor of it is in ids.
func under(it *model.Item, all []*model.Item, ids []string) bool {
	byID := map[string]*model.Item{}
	for _, x := range all {
		byID[x.ID] = x
	}
	seen := map[string]bool{}
	for p := it.Parent; p != "" && !seen[p]; {
		if slices.Contains(ids, p) {
			return true
		}
		seen[p] = true
		x := byID[p]
		if x == nil {
			return false
		}
		p = x.Parent
	}
	return false
}
