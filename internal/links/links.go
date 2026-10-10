// Package links ties tickets to the pull requests and commits that delivered
// them, using git and the GitHub CLI (gh). Agents create PRs; Sherpa finds
// and records them.
package links

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/RL-Charles/sherpa/internal/model"
)

// RePR matches a GitHub pull request URL.
var RePR = regexp.MustCompile(`https://github\.com/[\w.-]+/[\w.-]+/pull/(\d+)`)

// FromText returns the first pull request URL and number in s.
func FromText(s string) (string, int) {
	m := RePR.FindStringSubmatch(s)
	if m == nil {
		return "", 0
	}
	n, _ := strconv.Atoi(m[1])
	return m[0], n
}

type pr struct {
	Number       int    `json:"number"`
	URL          string `json:"url"`
	State        string `json:"state"`
	IsDraft      bool   `json:"isDraft"`
	HeadRefName  string `json:"headRefName"`
	Title        string `json:"title"`
	Body         string `json:"body"`
	Additions    int    `json:"additions"`
	Deletions    int    `json:"deletions"`
	ChangedFiles int    `json:"changedFiles"`
	Commits      []struct {
		Oid             string `json:"oid"`
		MessageHeadline string `json:"messageHeadline"`
	} `json:"commits"`
}

const prFields = "number,url,state,isDraft,headRefName,title,additions,deletions,changedFiles"

func gh(dir string, args ...string) ([]byte, error) {
	cmd := exec.Command("gh", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
			return nil, fmt.Errorf("gh %s: %s", args[0], strings.TrimSpace(string(ee.Stderr)))
		}
		return nil, err
	}
	return out, nil
}

// Available reports whether gh is installed.
func Available() bool {
	_, err := exec.LookPath("gh")
	return err == nil
}

func state(p pr) string {
	if p.IsDraft && strings.EqualFold(p.State, "open") {
		return "draft"
	}
	return strings.ToLower(p.State)
}

func apply(l *model.Links, p pr) {
	l.PR, l.PRNumber, l.PRState = p.URL, p.Number, state(p)
	if p.HeadRefName != "" {
		l.Branch = p.HeadRefName
	}
	if p.Additions+p.Deletions > 0 {
		l.Added, l.Removed, l.Files = p.Additions, p.Deletions, p.ChangedFiles
	}
	if len(p.Commits) > 0 {
		l.Commits = nil
		for _, c := range p.Commits {
			l.Commits = append(l.Commits, model.Commit{SHA: c.Oid, Subject: c.MessageHeadline})
		}
	}
}

// View fetches a PR (by URL or number) with its commits into l.
func View(dir, ref string, l *model.Links) error {
	out, err := gh(dir, "pr", "view", ref, "--json", prFields+",commits")
	if err != nil {
		return err
	}
	var p pr
	if err := json.Unmarshal(out, &p); err != nil {
		return err
	}
	apply(l, p)
	return nil
}

// ForBranch finds the PR for the branch checked out in dir.
func ForBranch(dir string, l *model.Links) error {
	return View(dir, "", l)
}

// List fetches every PR in the repository (newest first).
func List(dir string) ([]pr, error) {
	out, err := gh(dir, "pr", "list", "--state", "all", "--limit", "1000", "--json", prFields+",body")
	if err != nil {
		return nil, err
	}
	var prs []pr
	return prs, json.Unmarshal(out, &prs)
}

// MatchAll returns every PR that names an item, best first (merged first).
func MatchAll(prs []pr, keys []string) []pr {
	var out []pr
	rest := prs
	for {
		p, ok := Match(rest, keys)
		if !ok {
			return out
		}
		out = append(out, p)
		var next []pr
		for _, q := range rest {
			if q.Number != p.Number {
				next = append(next, q)
			}
		}
		rest = next
	}
}

// AllFromText returns every distinct PR URL in s, in order of appearance.
func AllFromText(s string) []string {
	var out []string
	for _, m := range RePR.FindAllString(s, -1) {
		if !slices.Contains(out, m) {
			out = append(out, m)
		}
	}
	return out
}

// Match finds the PR that delivered an item: a branch named after one of its
// keys (app-30-…, t-0030-…), or a title or body naming it. Merged PRs win.
func Match(prs []pr, keys []string) (pr, bool) {
	var best pr
	found := false
	for _, p := range prs {
		ranged := rangeKeys(p.Title)
		hit := false
		for _, k := range keys {
			re := regexp.MustCompile(`(?i)(^|[^a-z0-9])` + regexp.QuoteMeta(k) + `([^0-9]|$)`)
			if re.MatchString(p.HeadRefName) || re.MatchString(p.Title) || ranged[strings.ToLower(k)] {
				hit = true
			}
		}
		if !hit {
			continue
		}
		if !found || (state(p) == "merged" && state(best) != "merged") {
			best, found = p, true
		}
	}
	return best, found
}

// ApplyMatch records a matched PR (fetching its commits) on l.
func ApplyMatch(dir string, p pr, l *model.Links) error {
	apply(l, p)
	return View(dir, strconv.Itoa(p.Number), l)
}

// TrailerCommits lists commits whose message carries "Ticket: <id>".
func TrailerCommits(dir, id string) []model.Commit {
	cmd := exec.Command("git", "log", "--all", "--format=%H%x09%s", "--grep", "^Ticket: "+id+"$")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	var cs []model.Commit
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if sha, subj, ok := strings.Cut(line, "\t"); ok {
			cs = append(cs, model.Commit{SHA: sha, Subject: subj})
		}
	}
	return cs
}

// Keys are the names an item may go by in branches and PR titles: its ID and
// any imported key label (app-30 → APP-30).
func Keys(it *model.Item) []string {
	keys := []string{it.ID}
	for _, l := range it.Labels {
		if regexp.MustCompile(`^[a-z]+-\d+$`).MatchString(l) {
			keys = append(keys, l)
		}
	}
	return keys
}

var reKeyRange = regexp.MustCompile(`(?i)\b([a-z]+)-(\d+)\s*(?:\.\.\.?|–|—)\s*(?:[a-z]+-)?(\d+)\b`)

// rangeKeys expands "APP-72..78" or "APP-45–48" in a PR title into every key
// it covers: app-72, app-73 … app-78.
func rangeKeys(title string) map[string]bool {
	out := map[string]bool{}
	for _, m := range reKeyRange.FindAllStringSubmatch(title, -1) {
		lo, _ := strconv.Atoi(m[2])
		hi, _ := strconv.Atoi(m[3])
		if hi < lo || hi-lo > 50 {
			continue
		}
		for n := lo; n <= hi; n++ {
			out[strings.ToLower(m[1])+"-"+strconv.Itoa(n)] = true
		}
	}
	return out
}
