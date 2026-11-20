package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/spf13/cobra"

	"github.com/RL-Charles/sherpa/internal/actions"
	"github.com/RL-Charles/sherpa/internal/journal"
	"github.com/RL-Charles/sherpa/internal/live"
	"github.com/RL-Charles/sherpa/internal/model"
	"github.com/RL-Charles/sherpa/internal/render"
	"github.com/RL-Charles/sherpa/internal/store"
	"github.com/RL-Charles/sherpa/internal/worklog"
)

// The app feed is what the sherpa-app desktop viewer reads. It is plain JSON
// built from the same store, journal and live heartbeats as the TUI, so the
// desktop app never parses .sherpa/ itself and always agrees with the TUI on
// rollups, staleness, code-map reconciliation and worklog wording.
//
//	shp app snapshot   ──▶ project, every item (no body), live agents
//	shp app item <id>  ──▶ one item in full: body, structure, signals, worklog

type appProject struct {
	Name     string `json:"name"`
	Key      string `json:"key"`
	Root     string `json:"root"`
	Dir      string `json:"dir"`
	CodeRoot string `json:"code_root"`
	Active   string `json:"active,omitempty"`
}

type appRow struct {
	ID           string   `json:"id"`
	Kind         string   `json:"kind"`
	Title        string   `json:"title"`
	Status       string   `json:"status"`
	Parent       string   `json:"parent,omitempty"`
	Assignee     string   `json:"assignee,omitempty"`
	Priority     string   `json:"priority,omitempty"`
	Labels       []string `json:"labels,omitempty"`
	Depends      []string `json:"depends,omitempty"`
	Now          string   `json:"now,omitempty"`
	Next         string   `json:"next,omitempty"`
	Blocker      string   `json:"blocker,omitempty"`
	Updated      string   `json:"updated"`
	Age          string   `json:"age"`
	Stale        bool     `json:"stale,omitempty"`
	Security     bool     `json:"security,omitempty"`
	ManualOpen   int      `json:"manual_open,omitempty"`
	ChecksDone   int      `json:"checks_done"`
	ChecksTotal  int      `json:"checks_total"`
	DelivDone    int      `json:"deliverables_done"`
	DelivTotal   int      `json:"deliverables_total"`
	RollupDone   int      `json:"rollup_done"`
	RollupTotal  int      `json:"rollup_total"`
	PRNumber     int      `json:"pr_number,omitempty"`
	PRState      string   `json:"pr_state,omitempty"`
	CodeFiles    int      `json:"code_files,omitempty"`
	TemplateKind string   `json:"template,omitempty"`
}

type appPulse struct {
	Agent     string `json:"agent"`
	SessionID string `json:"session_id,omitempty"`
	Ticket    string `json:"ticket,omitempty"`
	Action    string `json:"action,omitempty"`
	File      string `json:"file,omitempty"`
	Worktree  string `json:"worktree,omitempty"`
	Branch    string `json:"branch,omitempty"`
	Seconds   int    `json:"seconds"`
}

type appSnapshot struct {
	Project   appProject     `json:"project"`
	Generated string         `json:"generated"`
	Items     []appRow       `json:"items"`
	Pulses    []appPulse     `json:"pulses"`
	Counts    map[string]int `json:"counts"`
	NeedsYou  int            `json:"needs_you"`
}

type appEntry struct {
	TS       string   `json:"ts"`
	Time     string   `json:"time"`
	Age      string   `json:"age"`
	Icon     string   `json:"icon"`
	Color    string   `json:"color"`
	Headline string   `json:"headline"`
	Detail   []string `json:"detail,omitempty"`
	Actor    string   `json:"actor,omitempty"`
	Files    []string `json:"files,omitempty"`
	Type     string   `json:"type"`
	Kind     string   `json:"kind,omitempty"`
	Routine  bool     `json:"routine,omitempty"`
}

type appSession struct {
	N            int        `json:"n"`
	Title        string     `json:"title"`
	Actor        string     `json:"actor"`
	AgentSession string     `json:"agent_session,omitempty"`
	Branch       string     `json:"branch,omitempty"`
	When         string     `json:"when"`
	Base         string     `json:"base,omitempty"`
	Head         string     `json:"head,omitempty"`
	Added        int        `json:"added,omitempty"`
	Removed      int        `json:"removed,omitempty"`
	Open         bool       `json:"open,omitempty"`
	Summary      string     `json:"summary,omitempty"`
	Touched      []string   `json:"touched,omitempty"`
	Entries      []appEntry `json:"entries"`
}

type appCrumb struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	Title  string `json:"title"`
	Status string `json:"status"`
}

type appReview struct {
	By        string   `json:"by"`
	At        string   `json:"at"`
	Message   string   `json:"message,omitempty"`
	Verified  string   `json:"verified,omitempty"`
	Followups []string `json:"followups,omitempty"`
}

type appItem struct {
	Row      appRow          `json:"row"`
	Item     *model.Item     `json:"item"`
	Body     string          `json:"body"`
	Outcome  string          `json:"outcome"`
	Crumbs   []appCrumb      `json:"crumbs"`
	Children []appRow        `json:"children"`
	Deps     []appCrumb      `json:"deps,omitempty"`
	Where    []model.CodeRef `json:"where"`
	OffPlan  []string        `json:"off_plan,omitempty"`
	Signals  []appEntry      `json:"signals"`
	Review   *appReview      `json:"review,omitempty"`
	Sessions []appSession    `json:"sessions"`
	Events   int             `json:"events"`
}

func (a *app) appCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "app",
		Short:  "JSON feed for the sherpa-app desktop viewer",
		Hidden: true,
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "snapshot",
		Short: "Project, every item and live agents as one JSON document",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.load(); err != nil {
				return err
			}
			snap, err := buildAppSnapshot(a.store, time.Now())
			if err != nil {
				return err
			}
			return a.emit(snap)
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "item <id>",
		Short: "One item in full: body, structure, signals and worklog",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.load(); err != nil {
				return err
			}
			item, err := buildAppItem(a.store, args[0])
			if err != nil {
				return err
			}
			return a.emit(item)
		},
	})
	cmd.AddCommand(a.appStatusCmd(), a.appApproveCmd(), a.appRejectCmd(), a.appEditCmd())
	return cmd
}

// The app's changes run the same shared actions as the TUI, as the person
// at the desktop, and answer with {"message": "..."} for the app to show.

type appResult struct {
	Message string `json:"message"`
	Merged  bool   `json:"merged,omitempty"`
	Body    string `json:"body,omitempty"`
}

func (a *app) who() actions.Who { return actions.Who{Actor: a.actor, Session: a.session} }

func (a *app) appStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use: "status <id> <status>", Short: "Move an item to a status, as the TUI's picker does", Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.load(); err != nil {
				return err
			}
			to, err := model.ParseStatus(args[1])
			if err != nil {
				return err
			}
			msg, err := actions.SetStatus(a.store, a.who(), args[0], to)
			if err != nil {
				return err
			}
			return a.emit(appResult{Message: msg})
		},
	}
}

func (a *app) appApproveCmd() *cobra.Command {
	var note string
	cmd := &cobra.Command{
		Use: "approve <id>", Short: "Approve reviewed work, as the TUI inbox does", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.load(); err != nil {
				return err
			}
			msg, err := actions.Approve(a.store, a.who(), args[0], note)
			if err != nil {
				return err
			}
			return a.emit(appResult{Message: msg})
		},
	}
	cmd.Flags().StringVarP(&note, "message", "m", "", "note for the journal")
	return cmd
}

func (a *app) appRejectCmd() *cobra.Command {
	var note string
	cmd := &cobra.Command{
		Use: "reject <id>", Short: "Send reviewed work back with a note", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.load(); err != nil {
				return err
			}
			msg, err := actions.SendBack(a.store, a.who(), args[0], note)
			if err != nil {
				return err
			}
			return a.emit(appResult{Message: msg})
		},
	}
	cmd.Flags().StringVarP(&note, "message", "m", "", "what needs fixing")
	return cmd
}

func (a *app) appEditCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "edit <id>",
		Short: `Save a body from stdin JSON {"base": ..., "body": ..., "force": bool}`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.load(); err != nil {
				return err
			}
			var in struct {
				Base  string `json:"base"`
				Body  string `json:"body"`
				Force bool   `json:"force"`
			}
			if err := json.NewDecoder(os.Stdin).Decode(&in); err != nil {
				return fmt.Errorf("read edit: %w", err)
			}
			body, merged, err := actions.SaveBody(a.store, a.who(), args[0], in.Base, in.Body, "the app", in.Force)
			if err != nil {
				return err
			}
			return a.emit(appResult{Message: "saved " + strings.ToUpper(args[0]), Merged: merged, Body: body})
		},
	}
}

func buildAppSnapshot(s *store.Store, now time.Time) (appSnapshot, error) {
	items, err := s.All()
	if err != nil {
		return appSnapshot{}, err
	}
	active, _ := s.Active()
	snap := appSnapshot{
		Project: appProject{
			Name: s.Config.Name, Key: s.Config.Key, Root: s.Root, Dir: s.Dir,
			CodeRoot: s.CodeRoot, Active: active,
		},
		Generated: now.UTC().Format(time.RFC3339),
		Items:     make([]appRow, 0, len(items)),
		Pulses:    []appPulse{},
		Counts:    map[string]int{},
	}
	for _, it := range items {
		snap.Items = append(snap.Items, appRowFor(it, items))
		if it.Kind == model.Ticket {
			snap.Counts[string(it.Status)]++
		}
		if it.Status == model.Review {
			snap.NeedsYou++
		}
	}
	for _, p := range live.Read(s.Dir, now) {
		snap.Pulses = append(snap.Pulses, appPulse{
			Agent: p.Agent, SessionID: p.SessionID, Ticket: p.Ticket, Action: p.Action,
			File: p.File, Worktree: p.Worktree, Branch: p.Branch, Seconds: int(now.Sub(p.TS).Seconds()),
		})
	}
	return snap, nil
}

func appRowFor(it *model.Item, all []*model.Item) appRow {
	r := appRow{
		ID: it.ID, Kind: string(it.Kind), Title: it.Title, Status: string(it.Status),
		Parent: it.Parent, Assignee: it.Assignee, Priority: it.Priority, Labels: it.Labels,
		Depends: it.Depends, Now: it.Now, Updated: it.Updated.UTC().Format(time.RFC3339),
		Age: render.Age(it.Updated), Stale: render.IsStale(it.Status, it.Updated),
		ManualOpen: len(it.OpenManual()), CodeFiles: len(it.Code), TemplateKind: it.Template,
	}
	if h := it.Handoff; h != nil {
		r.Next, r.Blocker = h.Next, h.Blocker
	}
	for _, l := range it.Labels {
		switch strings.ToLower(l) {
		case "security", "security-flagged", "needs-human":
			r.Security = true
		}
	}
	r.ChecksDone, r.ChecksTotal = it.CheckCounts()
	r.DelivDone, r.DelivTotal = it.DeliverableCounts()
	if it.Kind != model.Ticket {
		r.RollupDone, r.RollupTotal, _, _ = render.Rollup(it, all)
	}
	if l := it.Links; l != nil {
		r.PRNumber, r.PRState = l.PRNumber, l.PRState
	}
	return r
}

func appEntryFor(e journal.Event) appEntry {
	icon, color, head, detail := worklog.Describe(e)
	out := appEntry{
		TS: e.TS.UTC().Format(time.RFC3339), Time: e.TS.Local().Format("Jan 2 15:04"), Age: render.Age(e.TS),
		Icon: icon, Color: color, Headline: ansi.Strip(head), Actor: actorShort(e.Actor), Files: e.Files,
		Type: e.Type, Kind: e.Kind,
	}
	for _, d := range detail {
		out.Detail = append(out.Detail, ansi.Strip(d))
	}
	switch e.Type {
	case "log":
		out.Routine = e.Kind == "" || e.Kind == journal.KindProgress
	case "edit", "create", "where", "touch":
		out.Routine = true
	}
	return out
}

func actorShort(a string) string {
	if _, name, ok := strings.Cut(a, ":"); ok {
		return name
	}
	return a
}

func buildAppItem(s *store.Store, id string) (appItem, error) {
	it, err := s.Get(id)
	if err != nil {
		return appItem{}, err
	}
	all, err := s.All()
	if err != nil {
		return appItem{}, err
	}
	byID := make(map[string]*model.Item, len(all))
	for _, x := range all {
		byID[x.ID] = x
	}
	var events []journal.Event
	if evs, err := journal.ReadAll(s.Dir); err == nil {
		events = journal.ForItem(evs, it.ID)
	}
	out := appItem{
		Row: appRowFor(it, all), Item: it, Body: it.Body, Outcome: model.Outcome(it, 400),
		Crumbs: []appCrumb{}, Children: []appRow{}, Signals: []appEntry{}, Sessions: []appSession{},
		Events: len(events),
	}
	seen := map[string]bool{it.ID: true}
	for pid := it.Parent; pid != "" && !seen[pid]; {
		seen[pid] = true
		p := byID[pid]
		if p == nil {
			break
		}
		out.Crumbs = append([]appCrumb{{ID: p.ID, Kind: string(p.Kind), Title: p.Title, Status: string(p.Status)}}, out.Crumbs...)
		pid = p.Parent
	}
	for _, k := range store.Children(all, it.ID) {
		out.Children = append(out.Children, appRowFor(k, all))
	}
	for _, d := range it.Depends {
		c := appCrumb{ID: d, Status: "unknown"}
		if x := byID[d]; x != nil {
			c = appCrumb{ID: x.ID, Kind: string(x.Kind), Title: x.Title, Status: string(x.Status)}
		}
		out.Deps = append(out.Deps, c)
	}
	out.Where, out.OffPlan = model.ReconcileCode(it.Code, worklog.TouchedFiles(events))
	if out.Where == nil {
		out.Where = []model.CodeRef{}
	}
	for _, e := range worklog.RecentSignals(events, 4) {
		out.Signals = append(out.Signals, appEntryFor(e))
	}
	if it.Status == model.Review || it.Status == model.AgentReview {
		if e, ok := worklog.LastReview(events); ok {
			out.Review = &appReview{By: actorShort(e.Actor), At: e.TS.Local().Format("Jan 2 15:04"),
				Message: e.Message, Verified: e.Verified, Followups: e.Followups}
		}
	}
	sessions := worklog.Build(events)
	// Only the newest session can still be open: an older claim that never
	// reached submit was superseded by the next one, not left running.
	for i := len(sessions) - 1; i >= 0; i-- { // newest first, as the TUI shows them
		ss := sessions[i]
		title := fmt.Sprintf("Session %d", ss.N)
		if ss.N == 0 {
			title = "Planning"
		}
		as := appSession{
			N: ss.N, Title: title, Actor: actorShort(ss.Actor), AgentSession: ss.AgentSession, Branch: ss.Branch,
			When: appWhen(ss.Start, ss.End), Base: ss.Base, Head: ss.Head, Added: ss.Added, Removed: ss.Removed,
			Open: ss.Open && i == len(sessions)-1, Summary: worklog.Summary(ss), Touched: ss.Touched, Entries: []appEntry{},
		}
		for _, e := range ss.Entries {
			as.Entries = append(as.Entries, appEntryFor(e))
		}
		out.Sessions = append(out.Sessions, as)
	}
	return out, nil
}

func appWhen(start, end time.Time) string {
	start, end = start.Local(), end.Local()
	switch {
	case start.IsZero():
		return ""
	case end.Sub(start) < time.Minute:
		return start.Format("Jan 2 15:04")
	case start.YearDay() == end.YearDay() && start.Year() == end.Year():
		return start.Format("Jan 2 15:04") + "–" + end.Format("15:04")
	}
	return start.Format("Jan 2") + " → " + end.Format("Jan 2")
}
