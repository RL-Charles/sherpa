// Package importer converts other trackers into Sherpa items.
//
// Agency keeps one markdown file per issue (`APP-30.md`): YAML frontmatter,
// an H1 title, a body, then comments headed `## <author> · <RFC3339>`. A
// mapping file says which epic and story each issue belongs to; every issue
// must be placed exactly once.
package importer

import (
	"cmp"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/RL-Charles/sherpa/internal/journal"
	"github.com/RL-Charles/sherpa/internal/model"
	"github.com/RL-Charles/sherpa/internal/store"
)

// Mapping is the YAML file describing the target layout.
type Mapping struct {
	Key   string `yaml:"key"` // issue key prefix, e.g. APP
	Epics []struct {
		Title   string `yaml:"title"`
		Goal    string `yaml:"goal"`
		Stories []struct {
			Title       string `yaml:"title"`
			Description string `yaml:"description"`
			Issues      []int  `yaml:"issues"`
		} `yaml:"stories"`
	} `yaml:"epics"`
}

type Comment struct {
	Author string
	TS     time.Time
	Label  string // optional words after the timestamp: "started", "decision recorded"
	Text   string
}

type Issue struct {
	Key, Status, Title, Body string
	Num, Priority            int
	Created, Updated         time.Time
	Deps                     []int
	Comments                 []Comment
}

var (
	reComment = regexp.MustCompile(`(?m)^## (.+?) · (\d{4}-\d\d-\d\d(?:T\S+)?)[ \t]*(.*)$`)
	reTitle   = regexp.MustCompile(`(?m)^# (.+)$`)
	reDeps    = regexp.MustCompile(`(?ms)^## Dependencies\s*\n(.*?)(?:^## |\z)`)
	reAsset   = regexp.MustCompile(`\]\(assets/`)
	rePR      = regexp.MustCompile(`(?:PR #|/pull/)(\d+)`)
	reSHA     = regexp.MustCompile("`([0-9a-f]{7,40})`")
	reLink    = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
)

// ParseIssue reads one Agency issue file.
func ParseIssue(data []byte) (*Issue, error) {
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	if !strings.HasPrefix(text, "---\n") {
		return nil, fmt.Errorf("missing frontmatter")
	}
	end := strings.Index(text[4:], "\n---\n")
	if end < 0 {
		return nil, fmt.Errorf("unterminated frontmatter")
	}
	var fm struct {
		Key      string `yaml:"key"`
		Status   string `yaml:"status"`
		Priority int    `yaml:"priority"`
		Created  string `yaml:"created"`
		Updated  string `yaml:"updated"`
	}
	if err := yaml.Unmarshal([]byte(text[4:4+end]), &fm); err != nil {
		return nil, fmt.Errorf("frontmatter: %w", err)
	}
	rest := text[4+end+5:]
	m := reTitle.FindStringSubmatchIndex(rest)
	if m == nil {
		return nil, fmt.Errorf("%s: missing # title", fm.Key)
	}
	is := &Issue{Key: fm.Key, Status: fm.Status, Priority: fm.Priority, Title: strings.TrimSpace(rest[m[2]:m[3]])}
	if i := strings.LastIndex(fm.Key, "-"); i >= 0 {
		is.Num, _ = strconv.Atoi(fm.Key[i+1:])
	}
	if is.Num <= 0 {
		return nil, fmt.Errorf("issue key %q has no number", fm.Key)
	}
	is.Created, _ = time.Parse(time.RFC3339, fm.Created)
	is.Updated, _ = time.Parse(time.RFC3339, fm.Updated)
	afterTitle := rest[m[1]:]
	heads := reComment.FindAllStringSubmatchIndex(afterTitle, -1)
	bodyEnd := len(afterTitle)
	if len(heads) > 0 {
		bodyEnd = heads[0][0]
	}
	is.Body = strings.TrimSpace(afterTitle[:bodyEnd])
	for i, h := range heads {
		next := len(afterTitle)
		if i+1 < len(heads) {
			next = heads[i+1][0]
		}
		raw := afterTitle[h[4]:h[5]]
		ts, err := time.Parse(time.RFC3339, raw)
		if err != nil { // date-only heading: keep it after the previous comment that day
			ts, _ = time.Parse("2006-01-02", raw)
			if n := len(is.Comments); n > 0 && is.Comments[n-1].TS.After(ts) {
				ts = is.Comments[n-1].TS
			}
		}
		is.Comments = append(is.Comments, Comment{
			Author: strings.TrimSpace(afterTitle[h[2]:h[3]]),
			TS:     ts,
			Label:  strings.TrimSpace(afterTitle[h[6]:h[7]]),
			Text:   strings.TrimSpace(afterTitle[h[1]:next]),
		})
	}
	if d := reDeps.FindStringSubmatch(is.Body); d != nil {
		prefix := fm.Key[:strings.LastIndex(fm.Key, "-")+1]
		for _, k := range regexp.MustCompile(regexp.QuoteMeta(prefix)+`(\d+)`).FindAllStringSubmatch(d[1], -1) {
			n, _ := strconv.Atoi(k[1])
			is.Deps = append(is.Deps, n)
		}
	}
	return is, nil
}

// Result summarises an import.
type Result struct {
	Epics, Stories, Tickets int
	ByStatus                map[model.Status]int
	Events                  int
}

// Agency imports every issue under src into an empty store using mapping m.
func Agency(s *store.Store, src string, m *Mapping) (*Result, error) {
	existing, err := s.All()
	if err != nil {
		return nil, err
	}
	if len(existing) > 0 {
		return nil, fmt.Errorf("the Sherpa project already has %d items; import needs an empty project so ticket numbers can match issue numbers", len(existing))
	}
	files, err := filepath.Glob(filepath.Join(src, "*.md"))
	if err != nil {
		return nil, err
	}
	issues := map[int]*Issue{}
	for _, f := range files {
		if filepath.Base(f) == "README.md" {
			continue
		}
		data, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		is, err := ParseIssue(data)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", filepath.Base(f), err)
		}
		if m.Key != "" && !strings.HasPrefix(is.Key, m.Key+"-") {
			return nil, fmt.Errorf("%s has key %s, mapping expects %s-*", filepath.Base(f), is.Key, m.Key)
		}
		issues[is.Num] = is
	}
	if len(issues) == 0 {
		return nil, fmt.Errorf("no issues found in %s", src)
	}
	if err := validate(m, issues); err != nil {
		return nil, err
	}

	res := &Result{ByStatus: map[model.Status]int{}}
	status := func(is *Issue) model.Status {
		switch is.Status {
		case "done", "cancelled":
			return model.Done
		case "in_progress":
			return model.InProgress
		case "in_review":
			return model.Review
		case "todo":
			for _, d := range is.Deps {
				if dep, ok := issues[d]; ok && dep.Status != "done" {
					return model.Backlog // waiting on unfinished work
				}
			}
			return model.Ready
		}
		return model.Backlog
	}
	var events []journal.Event
	ev := func(e journal.Event) {
		e.Session = "import-agency"
		events = append(events, e)
	}

	for ei, ep := range m.Epics {
		epicID := model.FormatID(model.Epic, ei+1)
		var epicChildren []model.Status
		var epicSpan []time.Time
		for _, st := range ep.Stories {
			res.Stories++
			storyID := model.FormatID(model.Story, res.Stories)
			var children []model.Status
			var span []time.Time
			for _, n := range st.Issues {
				is := issues[n]
				t := ticket(is, storyID, status(is))
				if err := s.Write(t); err != nil {
					return nil, err
				}
				res.Tickets++
				res.ByStatus[t.Status]++
				children = append(children, t.Status)
				span = append(span, is.Created, is.Updated)
				history(is, t, ev)
			}
			story := &model.Item{ID: storyID, Kind: model.Story, Title: st.Title, Parent: epicID,
				Status: rollup(children), Created: earliest(span), Updated: latest(span),
				Body: model.DefaultBody(model.Story, strings.TrimSpace(st.Description))}
			story.Body = strings.TrimSuffix(story.Body, "\n\n## Acceptance criteria\n\n- [ ] \n") + "\n"
			if err := s.Write(story); err != nil {
				return nil, err
			}
			epicChildren = append(epicChildren, story.Status)
			epicSpan = append(epicSpan, span...)
			ev(journal.Event{TS: story.Created, Actor: "system:agency", Item: storyID, Type: "create", Message: st.Title})
		}
		epic := &model.Item{ID: epicID, Kind: model.Epic, Title: ep.Title, Status: rollup(epicChildren),
			Created: earliest(epicSpan), Updated: latest(epicSpan),
			Body: model.DefaultBody(model.Epic, strings.TrimSpace(ep.Goal))}
		if err := s.Write(epic); err != nil {
			return nil, err
		}
		res.Epics++
		ev(journal.Event{TS: epic.Created, Actor: "system:agency", Item: epicID, Type: "create", Message: ep.Title})
	}

	sort.SliceStable(events, func(i, j int) bool { return events[i].TS.Before(events[j].TS) })
	for _, e := range events {
		if err := journal.Append(s.Dir, e); err != nil {
			return nil, err
		}
	}
	res.Events = len(events)
	return res, archive(src, filepath.Join(s.Dir, "imports", "agency"))
}

func validate(m *Mapping, issues map[int]*Issue) error {
	seen := map[int]string{}
	var problems []string
	for _, ep := range m.Epics {
		for _, st := range ep.Stories {
			for _, n := range st.Issues {
				if _, ok := issues[n]; !ok {
					problems = append(problems, fmt.Sprintf("%s lists issue %d, which does not exist", st.Title, n))
				}
				if prev, dup := seen[n]; dup {
					problems = append(problems, fmt.Sprintf("issue %d is in both %q and %q", n, prev, st.Title))
				}
				seen[n] = st.Title
			}
		}
	}
	var missing []string
	for n, is := range issues {
		if _, ok := seen[n]; !ok {
			missing = append(missing, is.Key)
		}
	}
	slices.Sort(missing)
	if len(missing) > 0 {
		problems = append(problems, "not placed in any story: "+strings.Join(missing, ", "))
	}
	if len(problems) > 0 {
		return fmt.Errorf("mapping problems:\n  %s", strings.Join(problems, "\n  "))
	}
	return nil
}

func ticket(is *Issue, parent string, st model.Status) *model.Item {
	t := &model.Item{
		ID:       model.FormatID(model.Ticket, is.Num),
		Kind:     model.Ticket,
		Title:    is.Title,
		Status:   st,
		Parent:   parent,
		Priority: "p" + strconv.Itoa(is.Priority),
		Labels:   []string{strings.ToLower(is.Key)},
		Created:  is.Created,
		Updated:  cmp.Or(is.Updated, is.Created),
		// Attachments are archived beside the originals; keep links working.
		Body: reAsset.ReplaceAllString(is.Body, "](../imports/agency/assets/") + "\n",
	}
	if (st == model.InProgress || st == model.Review) && len(is.Comments) > 0 {
		t.Assignee = slug(is.Comments[len(is.Comments)-1].Author)
	}
	return t
}

// history turns comments into a worklog session and the final status.
func history(is *Issue, t *model.Item, ev func(journal.Event)) {
	ev(journal.Event{TS: is.Created, Actor: "system:agency", Item: t.ID, Type: "create", Message: is.Title})
	started := t.Status == model.Done || t.Status == model.InProgress || t.Status == model.Review
	for i, c := range is.Comments {
		if i == 0 && started {
			ev(journal.Event{TS: c.TS, Actor: actor(c.Author), Item: t.ID, Type: "claim", From: string(model.Ready)})
		}
		head, detail := summarize(c.Text)
		if c.Label != "" {
			head = strings.ToUpper(c.Label[:1]) + c.Label[1:] + ": " + head
			if r := []rune(head); len(r) > 160 {
				head = string(r[:159]) + "…"
			}
		}
		kind := commentKind(c.Label + " " + head)
		ev(journal.Event{TS: c.TS, Actor: actor(c.Author), Item: t.ID, Type: "log", Kind: kind, Message: head, Detail: detail})
	}
	switch t.Status {
	case model.Done, model.Review:
		ev(journal.Event{TS: t.Updated, Actor: "system:agency", Item: t.ID, Type: "status",
			From: string(model.InProgress), To: string(t.Status), Message: "imported from Agency " + is.Key})
	}
}

// summarize reduces a comment to a one-line headline plus PR/commit refs.
func summarize(text string) (string, []string) {
	plain := reLink.ReplaceAllString(text, "$1")
	plain = strings.NewReplacer("**", "", "`", "", "\n", " ").Replace(plain)
	plain = strings.Join(strings.Fields(plain), " ")
	head := plain
	if i := strings.Index(head, ". "); i > 0 {
		head = head[:i+1]
	}
	if r := []rune(head); len(r) > 140 {
		head = string(r[:139]) + "…"
	}
	var refs []string
	seen := map[string]bool{}
	for _, m := range rePR.FindAllStringSubmatch(text, -1) {
		if r := "PR #" + m[1]; !seen[r] {
			seen[r] = true
			refs = append(refs, r)
		}
	}
	for _, m := range reSHA.FindAllStringSubmatch(text, -1) {
		if r := m[1][:min(7, len(m[1]))]; !seen[r] {
			seen[r] = true
			refs = append(refs, r)
		}
	}
	if len(refs) > 6 {
		refs = append(refs[:6], fmt.Sprintf("+%d more", len(refs)-6))
	}
	if len(refs) == 0 {
		return head, nil
	}
	return head, []string{strings.Join(refs, " · ")}
}

// commentKind guesses an entry kind from a comment's label and headline.
func commentKind(s string) string {
	s = strings.ToLower(s)
	switch {
	case strings.Contains(s, "block"):
		return journal.KindBlocker
	case strings.Contains(s, "decision") || strings.Contains(s, "decided"):
		return journal.KindDecision
	case strings.Contains(s, "verified") || strings.Contains(s, "qualified"):
		return journal.KindVerify
	}
	return journal.KindProgress
}

var agentNames = []string{"codex", "claude", "cursor", "grok", "gemini", "copilot", "gpt", "agent"}

func actor(author string) string {
	name := slug(author)
	for _, a := range agentNames {
		if strings.Contains(name, a) {
			return "agent:" + name
		}
	}
	return "human:" + name
}

func slug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	return strings.Trim(regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(s, "-"), "-")
}

// rollup derives a parent's status from its children.
func rollup(children []model.Status) model.Status {
	if len(children) == 0 {
		return model.Backlog
	}
	done, started, ready := 0, false, false
	for _, c := range children {
		switch c {
		case model.Done:
			done++
			started = true
		case model.InProgress, model.Review, model.Blocked:
			started = true
		case model.Ready:
			ready = true
		}
	}
	switch {
	case done == len(children):
		return model.Done
	case started:
		return model.InProgress
	case ready:
		return model.Ready
	}
	return model.Backlog
}

func earliest(ts []time.Time) time.Time {
	var out time.Time
	for _, t := range ts {
		if !t.IsZero() && (out.IsZero() || t.Before(out)) {
			out = t
		}
	}
	return cmp.Or(out, time.Now().UTC().Truncate(time.Second))
}

func latest(ts []time.Time) time.Time {
	var out time.Time
	for _, t := range ts {
		if t.After(out) {
			out = t
		}
	}
	return cmp.Or(out, time.Now().UTC().Truncate(time.Second))
}

// archive copies the original issue files and attachments verbatim.
func archive(src, dst string) error {
	return filepath.Walk(src, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if fi.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.Create(target)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, in); err != nil {
			out.Close()
			return err
		}
		return out.Close()
	})
}
