// Package journal is Sherpa's append-only audit log. Each session writes its
// own JSONL file so parallel agents never conflict in git.
package journal

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

type Event struct {
	TS      time.Time `json:"ts"`
	Actor   string    `json:"actor"`
	Session string    `json:"session"`
	Item    string    `json:"item,omitempty"`
	Type    string    `json:"type"`           // create, status, claim, log, submit, edit
	Kind    string    `json:"kind,omitempty"` // log entries: progress, decision, verify, blocker
	From    string    `json:"from,omitempty"`
	To      string    `json:"to,omitempty"`
	Message string    `json:"message,omitempty"` // one-line headline
	Detail  []string  `json:"detail,omitempty"`  // optional supporting lines
	Files   []string  `json:"files,omitempty"`

	// Submit only: how the work was verified, and what is left to do.
	Verified  string   `json:"verified,omitempty"`
	Followups []string `json:"followups,omitempty"`

	// Git context when the event was recorded. Commit is HEAD (short sha);
	// Added/Removed are line counts for Files relative to the commit the
	// work session started from, including uncommitted changes.
	Commit  string `json:"commit,omitempty"`
	Added   int    `json:"added,omitempty"`
	Removed int    `json:"removed,omitempty"`
}

// Log entry kinds.
const (
	KindProgress = "progress"
	KindDecision = "decision"
	KindVerify   = "verify"
	KindBlocker  = "blocker"
)

var Kinds = []string{KindProgress, KindDecision, KindVerify, KindBlocker}

// ForItem returns the events about one item, oldest first.
func ForItem(events []Event, id string) []Event {
	var out []Event
	for _, e := range events {
		if e.Item == id {
			out = append(out, e)
		}
	}
	return out
}

var unsafe = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// Session identifies the current working session: $SHERPA_SESSION if set,
// otherwise one file per actor per day.
func Session(actor string) string {
	if s := strings.TrimSpace(os.Getenv("SHERPA_SESSION")); s != "" {
		return unsafe.ReplaceAllString(s, "-")
	}
	return unsafe.ReplaceAllString(actor, "-") + "-" + time.Now().Format("20060102")
}

func Append(sherpaDir string, e Event) error {
	if e.TS.IsZero() {
		e.TS = time.Now().UTC().Truncate(time.Second)
	}
	data, err := json.Marshal(e)
	if err != nil {
		return err
	}
	dir := filepath.Join(sherpaDir, "sessions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(dir, e.Session+".jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(data, '\n'))
	return err
}

// ReadAll returns every event from every session, oldest first.
func ReadAll(sherpaDir string) ([]Event, error) {
	files, err := filepath.Glob(filepath.Join(sherpaDir, "sessions", "*.jsonl"))
	if err != nil {
		return nil, err
	}
	var events []Event
	for _, path := range files {
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			var e Event
			if json.Unmarshal(sc.Bytes(), &e) == nil {
				events = append(events, e)
			}
		}
		f.Close()
		if err := sc.Err(); err != nil {
			return nil, err
		}
	}
	sort.SliceStable(events, func(i, j int) bool { return events[i].TS.Before(events[j].TS) })
	return events, nil
}
