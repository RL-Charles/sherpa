// Package model defines Sherpa's work items: epics, stories and tickets.
package model

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

type Kind string

const (
	Epic   Kind = "epic"
	Story  Kind = "story"
	Ticket Kind = "ticket"
)

var Kinds = []Kind{Epic, Story, Ticket}

func (k Kind) Prefix() string {
	switch k {
	case Epic:
		return "E"
	case Story:
		return "S"
	default:
		return "T"
	}
}

// Width is the zero-padded width of the numeric part of an ID.
func (k Kind) Width() int {
	if k == Ticket {
		return 4
	}
	return 3
}

// Dir is the directory under .sherpa/ holding items of this kind.
func (k Kind) Dir() string {
	if k == Story {
		return "stories"
	}
	return string(k) + "s"
}

// ParentKind returns the kind an item of this kind must hang under.
func (k Kind) ParentKind() (Kind, bool) {
	switch k {
	case Story:
		return Epic, true
	case Ticket:
		return Story, true
	}
	return "", false
}

func ParseKind(s string) (Kind, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "epic", "epics", "e":
		return Epic, nil
	case "story", "stories", "s":
		return Story, nil
	case "ticket", "tickets", "t":
		return Ticket, nil
	}
	return "", fmt.Errorf("unknown kind %q (want epic, story or ticket)", s)
}

// FormatID builds an ID such as T-0012.
func FormatID(k Kind, n int) string {
	return fmt.Sprintf("%s-%0*d", k.Prefix(), k.Width(), n)
}

// ParseID normalises an ID ("t12", "T-12", "T-0012") and returns its kind and number.
func ParseID(id string) (string, Kind, int, error) {
	s := strings.ToUpper(strings.TrimSpace(id))
	if len(s) < 2 {
		return "", "", 0, fmt.Errorf("invalid id %q", id)
	}
	var k Kind
	switch s[0] {
	case 'E':
		k = Epic
	case 'S':
		k = Story
	case 'T':
		k = Ticket
	default:
		return "", "", 0, fmt.Errorf("invalid id %q (want E-, S- or T- prefix)", id)
	}
	n, err := strconv.Atoi(strings.TrimPrefix(s[1:], "-"))
	if err != nil || n <= 0 {
		return "", "", 0, fmt.Errorf("invalid id %q", id)
	}
	return FormatID(k, n), k, n, nil
}

type Status string

const (
	Backlog     Status = "backlog"
	Ready       Status = "ready"
	InProgress  Status = "in_progress"
	AgentReview Status = "agent_review" // submitted by an agent; a human or another agent reviews it first
	Review      Status = "review"       // awaiting human review
	Blocked     Status = "blocked"
	Done        Status = "done"
)

var Statuses = []Status{Backlog, Ready, InProgress, AgentReview, Review, Blocked, Done}

func ParseStatus(s string) (Status, error) {
	norm := strings.ReplaceAll(strings.ToLower(strings.TrimSpace(s)), "-", "_")
	if norm == "wip" || norm == "doing" || norm == "inprogress" {
		norm = string(InProgress)
	}
	for _, st := range Statuses {
		if string(st) == norm {
			return st, nil
		}
	}
	return "", fmt.Errorf("unknown status %q (want one of %s)", s, joinStatuses())
}

func joinStatuses() string {
	parts := make([]string, len(Statuses))
	for i, s := range Statuses {
		parts[i] = string(s)
	}
	return strings.Join(parts, ", ")
}

// Glyph is a one-character marker for the status in compact views.
func (s Status) Glyph() string {
	switch s {
	case Backlog:
		return "○"
	case Ready:
		return "◎"
	case InProgress:
		return "◐"
	case AgentReview:
		return "◒"
	case Review:
		return "◑"
	case Blocked:
		return "✖"
	case Done:
		return "●"
	}
	return "?"
}

// Item is one epic, story or ticket. Frontmatter fields live in the YAML
// header of its markdown file; Body is everything after it.
type Item struct {
	ID       string    `yaml:"id" json:"id"`
	Kind     Kind      `yaml:"kind" json:"kind"`
	Title    string    `yaml:"title" json:"title"`
	Status   Status    `yaml:"status" json:"status"`
	Parent   string    `yaml:"parent,omitempty" json:"parent,omitempty"`
	Assignee string    `yaml:"assignee,omitempty" json:"assignee,omitempty"`
	Priority string    `yaml:"priority,omitempty" json:"priority,omitempty"`
	Labels   []string  `yaml:"labels,omitempty" json:"labels,omitempty"`
	Depends  []string  `yaml:"depends,omitempty" json:"depends,omitempty"`
	Now      string    `yaml:"now,omitempty" json:"now,omitempty"` // one-line current state
	Created  time.Time `yaml:"created" json:"created"`
	Updated  time.Time `yaml:"updated" json:"updated"`
	Checks   []Check   `yaml:"checks,omitempty" json:"checks,omitempty"`
	Measures []Measure `yaml:"measures,omitempty" json:"measures,omitempty"`
	Links    *Links    `yaml:"links,omitempty" json:"links,omitempty"`
	Body     string    `yaml:"-" json:"body,omitempty"`
}

// DefaultBody is the markdown skeleton for a new item of kind k. The
// worklog is not part of it: it is generated from the journal.
func DefaultBody(k Kind, description string) string {
	if description == "" {
		description = "_No description yet._"
	}
	switch k {
	case Epic:
		return "## Goal\n\n" + description + "\n"
	default:
		// Acceptance criteria are structured checks (shp check), not body text.
		return "## Description\n\n" + description + "\n"
	}
}
