package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/spf13/cobra"
	"golang.org/x/term"
	"gopkg.in/yaml.v3"

	"github.com/RL-Charles/sherpa/internal/gitinfo"
	"github.com/RL-Charles/sherpa/internal/importer"
	"github.com/RL-Charles/sherpa/internal/journal"
	"github.com/RL-Charles/sherpa/internal/links"
	"github.com/RL-Charles/sherpa/internal/model"
	"github.com/RL-Charles/sherpa/internal/render"
	"github.com/RL-Charles/sherpa/internal/store"
	"github.com/RL-Charles/sherpa/internal/workflow"
	"github.com/RL-Charles/sherpa/internal/worklog"
)

func (a *app) initCmd() *cobra.Command {
	var key, name string
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Create a .sherpa directory in the current repo",
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			if name == "" {
				name = filepath.Base(cwd)
			}
			if key == "" {
				key = strings.ToUpper(name)
				if len(key) > 4 {
					key = key[:4]
				}
			}
			s, err := store.Init(cwd, key, name)
			if err != nil {
				return err
			}
			if a.json {
				return a.emit(s.Config)
			}
			fmt.Printf("Initialised Sherpa project %s (%s) in %s\n", s.Config.Name, s.Config.Key, s.Dir)
			return nil
		},
	}
	cmd.Flags().StringVar(&key, "key", "", "short project key, e.g. SHP")
	cmd.Flags().StringVar(&name, "name", "", "project name (default: directory name)")
	return cmd
}

func (a *app) newCmd() *cobra.Command {
	var n store.NewItem
	var checks, manualChecks, goals, deliverables []string
	var template string
	cmd := &cobra.Command{
		Use:   "new <epic|story|ticket> <title...>",
		Short: "Create an epic, story or ticket",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.load(); err != nil {
				return err
			}
			k, err := model.ParseKind(args[0])
			if err != nil {
				return err
			}
			n.Kind = k
			n.Title = strings.Join(args[1:], " ")
			if template != "" && k != model.Ticket {
				return errors.New("--template is for tickets; epics and stories have their own shape")
			}
			tplBody := ""
			if template != "" {
				var err error
				if tplBody, err = model.TemplateBody(template); err != nil {
					return err
				}
			}
			if len(goals) > 0 {
				if k != model.Epic {
					return errors.New("--goal is for epics; stories and tickets use --check")
				}
				n.Description = strings.TrimSpace(n.Description + "\n\n" + model.GoalList(goals))
			}
			if k == model.Epic {
				draft := &model.Item{ID: "the epic", Kind: k, Body: model.DefaultBody(k, n.Description)}
				if p := model.GoalProblem(draft); p != "" {
					return errors.New(p + `: add them with -g "…" -g "…"`)
				}
			}
			it, err := a.store.Create(n)
			if err != nil {
				return err
			}
			changed := false
			if template != "" {
				it.Body = model.FillTemplate(tplBody, n.Description)
				it.Template = strings.ToLower(strings.TrimSpace(template))
				changed = true
			}
			for _, d := range deliverables {
				if d = strings.TrimSpace(d); d != "" {
					it.Deliverables = append(it.Deliverables, model.Deliverable{Text: d})
					changed = true
				}
			}
			if len(checks)+len(manualChecks) > 0 {
				for _, c := range checks {
					it.Checks = append(it.Checks, model.Check{Text: c})
				}
				for _, c := range manualChecks {
					it.Checks = append(it.Checks, model.Check{Text: c, Manual: true})
				}
				changed = true
			}
			if changed {
				if err := a.store.Save(it); err != nil {
					return err
				}
			}
			if err := a.record(journal.Event{Item: it.ID, Type: "create", Message: it.Title}); err != nil {
				return err
			}
			if a.json {
				return a.emit(it)
			}
			fmt.Println("Created", render.Line(it))
			return nil
		},
	}
	cmd.Flags().StringVarP(&n.Parent, "parent", "p", "", "parent epic (for a story) or story (for a ticket)")
	cmd.Flags().StringVarP(&n.Description, "message", "m", "", "description (markdown)")
	cmd.Flags().StringVar(&n.Priority, "priority", "", "p0-p3")
	cmd.Flags().StringSliceVarP(&n.Labels, "label", "l", nil, "labels (repeatable)")
	cmd.Flags().StringVar(&template, "template", "", "ticket body template: "+strings.Join(model.TemplateNames, ", "))
	cmd.Flags().StringArrayVarP(&deliverables, "deliverable", "d", nil, "outcome the ticket delivers (repeatable)")
	cmd.Flags().StringArrayVarP(&checks, "check", "c", nil, "acceptance check (repeatable)")
	cmd.Flags().StringArrayVar(&manualChecks, "manual-check", nil, "check a human verifies by hand (repeatable)")
	cmd.Flags().StringArrayVarP(&goals, "goal", "g", nil, fmt.Sprintf("epic deliverable, one bullet each (repeatable, at least %d)", model.MinGoals))
	return cmd
}

func (a *app) lsCmd() *cobra.Command {
	var kind, status, assignee, parent string
	cmd := &cobra.Command{
		Use:     "ls",
		Aliases: []string{"list"},
		Short:   "List items as a tree, or filtered as a flat list",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.load(); err != nil {
				return err
			}
			items, err := a.store.All()
			if err != nil {
				return err
			}
			filtered := kind != "" || status != "" || assignee != "" || parent != ""
			if filtered {
				items, err = filter(items, kind, status, assignee, parent)
				if err != nil {
					return err
				}
			}
			if a.json {
				for _, it := range items {
					it.Body = ""
				}
				return a.emit(items)
			}
			if len(items) == 0 {
				fmt.Println("Nothing here yet. Create an epic: shp new epic \"…\"")
				return nil
			}
			if filtered {
				for _, it := range items {
					fmt.Println(render.Line(it))
				}
				return nil
			}
			render.Tree(os.Stdout, items)
			return nil
		},
	}
	cmd.Flags().StringVarP(&kind, "kind", "k", "", "epic, story or ticket")
	cmd.Flags().StringVarP(&status, "status", "s", "", "comma-separated statuses")
	cmd.Flags().StringVarP(&assignee, "assignee", "a", "", "assignee name")
	cmd.Flags().StringVarP(&parent, "parent", "p", "", "direct children of this ID")
	return cmd
}

func filter(items []*model.Item, kind, status, assignee, parent string) ([]*model.Item, error) {
	var k model.Kind
	if kind != "" {
		var err error
		if k, err = model.ParseKind(kind); err != nil {
			return nil, err
		}
	}
	statuses := map[model.Status]bool{}
	if status != "" {
		for _, s := range strings.Split(status, ",") {
			st, err := model.ParseStatus(s)
			if err != nil {
				return nil, err
			}
			statuses[st] = true
		}
	}
	if parent != "" {
		norm, _, _, err := model.ParseID(parent)
		if err != nil {
			return nil, err
		}
		parent = norm
	}
	var out []*model.Item
	for _, it := range items {
		if (k == "" || it.Kind == k) &&
			(len(statuses) == 0 || statuses[it.Status]) &&
			(assignee == "" || strings.EqualFold(it.Assignee, assignee)) &&
			(parent == "" || it.Parent == parent) {
			out = append(out, it)
		}
	}
	return out, nil
}

func (a *app) showCmd() *cobra.Command {
	var raw, expandAll bool
	cmd := &cobra.Command{
		Use:   "show [id]",
		Short: "Show an item with rendered markdown (defaults to the active ticket)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.load(); err != nil {
				return err
			}
			it, err := a.resolve(args)
			if err != nil {
				return err
			}
			events, err := journal.ReadAll(a.store.Dir)
			if err != nil {
				return err
			}
			sessions := worklog.Build(journal.ForItem(events, it.ID))
			if a.json {
				return a.emit(struct {
					*model.Item
					Worklog []worklog.Session `json:"worklog"`
				}{it, sessions})
			}
			if raw {
				data, err := store.Render(it)
				if err != nil {
					return err
				}
				_, err = os.Stdout.Write(data)
				return err
			}
			var parent *model.Item
			if it.Parent != "" {
				parent, _ = a.store.Get(it.Parent)
			}
			all, err := a.store.All()
			if err != nil {
				return err
			}
			doc := render.Document(it, parent, store.Children(all, it.ID), all...)
			if e, ok := worklog.LastReview(journal.ForItem(events, it.ID)); ok {
				if card := render.ReviewCard(it, e); card != "" {
					head := render.DocHeader(it, parent, all...)
					doc = head + card + strings.TrimPrefix(doc, head)
				}
			}
			fmt.Print(render.Markdown(doc))
			width := 100
			if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 20 {
				width = min(w, 100)
			}
			fmt.Println()
			hideSubmit := it.Status == model.Review || it.Status == model.AgentReview
			for _, l := range worklog.RenderOptions(sessions, width, worklog.Options{ExpandAll: expandAll, HideSubmitDetail: hideSubmit}) {
				fmt.Println(l)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&raw, "raw", false, "print the underlying markdown file")
	cmd.Flags().BoolVar(&expandAll, "all", false, "expand every worklog session")
	return cmd
}

func (a *app) statusCmd() *cobra.Command {
	var note string
	cmd := &cobra.Command{
		Use:     "status <id> <status>",
		Aliases: []string{"mv"},
		Short:   "Move an item to a new status",
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.load(); err != nil {
				return err
			}
			it, err := a.store.Get(args[0])
			if err != nil {
				return err
			}
			to, err := model.ParseStatus(args[1])
			if err != nil {
				return err
			}
			if err := a.transition(it, to, note); err != nil {
				return err
			}
			return a.done(it)
		},
	}
	cmd.Flags().StringVarP(&note, "message", "m", "", "note for the worklog")
	return cmd
}

func (a *app) startCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "start <id>",
		Short: "Claim a ticket: assign it to you, mark in_progress, make it active",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.load(); err != nil {
				return err
			}
			it, err := a.store.Get(args[0])
			if err != nil {
				return err
			}
			if it.Kind != model.Ticket {
				return fmt.Errorf("%s is a %s; only tickets are started (pick one of its tickets)", it.ID, it.Kind)
			}
			if it.Status == model.InProgress && it.Assignee != "" && it.Assignee != a.actor.Name {
				return fmt.Errorf("%s is already in progress by %s", it.ID, it.Assignee)
			}
			from := it.Status
			it.Assignee = a.actor.Name
			if it.Status != model.InProgress {
				if _, err := a.setStatus(it, model.InProgress); err != nil {
					return err
				}
			} else if err := a.store.Save(it); err != nil {
				return err
			}
			if err := a.setActive(it.ID); err != nil {
				return err
			}
			// One claim event opens a work session; its commit is the session's
			// base for diff stats.
			if err := a.record(journal.Event{Item: it.ID, Type: "claim", From: string(from), To: string(model.InProgress),
				Commit: gitinfo.Head(a.store.CodeRoot)}); err != nil {
				return err
			}
			return a.done(it)
		},
	}
}

func (a *app) logCmd() *cobra.Command {
	var id, kind string
	var files, detail []string
	cmd := &cobra.Command{
		Use:   "log <headline...>",
		Short: "Add a worklog entry to the active ticket (or --ticket)",
		Long: `Add a worklog entry. Keep the headline to one line; put supporting
facts in --detail lines and touched files in --file.

Kinds: progress (default), decision, verify, blocker.

  shp log "Parse tables with lipgloss" -f internal/tui/table.go
  shp log -k decision "Line splitter, not goldmark" -d "goldmark lists have no source positions"
  shp log -k verify "go test ./... passes; tmux 80x24 checked"
  shp log -k blocker "Need an API key for the MCP smoke test"`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.load(); err != nil {
				return err
			}
			if !slices.Contains(journal.Kinds, kind) {
				return fmt.Errorf("unknown kind %q (want %s)", kind, strings.Join(journal.Kinds, ", "))
			}
			it, err := a.resolve([]string{id})
			if err != nil {
				return err
			}
			if a.actor.Kind == workflow.Agent && it.Kind == model.Ticket && it.Status != model.InProgress {
				return fmt.Errorf("%s is %s; claim it first with `shp start %s` so the work lands in a session", it.ID, it.Status, it.ID)
			}
			base, _, err := a.workSession(it.ID)
			if err != nil {
				return err
			}
			added, removed := gitinfo.DiffStat(a.store.CodeRoot, base, files)
			e := journal.Event{Item: it.ID, Type: "log", Kind: kind, Message: strings.Join(args, " "),
				Detail: detail, Files: files, Commit: gitinfo.Head(a.store.CodeRoot), Added: added, Removed: removed}
			if err := a.record(e); err != nil {
				return err
			}
			if a.json {
				return a.emit(e)
			}
			icon, _, _, _ := worklog.Describe(e)
			fmt.Printf("%s %s %s\n", icon, it.ID, e.Message)
			return nil
		},
	}
	cmd.Flags().StringVarP(&id, "ticket", "t", "", "item to log against (default: active ticket)")
	cmd.Flags().StringVarP(&kind, "kind", "k", journal.KindProgress, "progress, decision, verify or blocker")
	cmd.Flags().StringArrayVarP(&detail, "detail", "d", nil, "supporting line under the headline (repeatable)")
	cmd.Flags().StringSliceVarP(&files, "file", "f", nil, "files touched (repeatable)")
	return cmd
}

func (a *app) submitCmd() *cobra.Command {
	var summary, verified string
	var followups, files []string
	cmd := &cobra.Command{
		Use:   "submit [id]",
		Short: "Finish work on a ticket and send it for human review",
		Long: `Send a ticket for review with a short PR-style summary.

  shp submit -m "Boxed tables via lipgloss" \
    --verified "go test ./...; tmux 150 and 80 cols" \
    --followup "shp show still uses Glamour tables"

Agents must say how the work was verified.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.load(); err != nil {
				return err
			}
			it, err := a.resolve(args)
			if err != nil {
				return err
			}
			if strings.TrimSpace(summary) == "" {
				return errors.New("a summary of what changed is required (-m \"…\")")
			}
			if a.actor.Kind == workflow.Agent && strings.TrimSpace(verified) == "" {
				return errors.New("agents must say how the work was verified (--verified \"…\")")
			}
			base, sessionFiles, err := a.workSession(it.ID)
			if err != nil {
				return err
			}
			for _, f := range files {
				if !slices.Contains(sessionFiles, f) {
					sessionFiles = append(sessionFiles, f)
				}
			}
			a.linkWork(it)
			to := model.Review
			if a.actor.Kind == workflow.Agent { // a human or another agent reviews it first
				to = model.AgentReview
			}
			from, err := a.setStatus(it, to)
			if err != nil {
				return err
			}
			warnings := reviewWarnings(it)
			added, removed := gitinfo.DiffStat(a.store.CodeRoot, base, sessionFiles)
			if err := a.record(journal.Event{Item: it.ID, Type: "submit", From: string(from), To: string(to),
				Message: summary, Verified: verified, Followups: followups, Files: sessionFiles, Detail: warnings,
				Commit: gitinfo.Head(a.store.CodeRoot), Added: added, Removed: removed}); err != nil {
				return err
			}
			for _, w := range warnings { // never blocks; the reviewer sees these too
				fmt.Fprintln(os.Stderr, w)
			}
			if active, _ := a.activeID(); active == it.ID {
				if err := a.setActive(""); err != nil {
					return err
				}
			}
			if to == model.AgentReview {
				a.autoReview(it)
			}
			return a.done(it)
		},
	}
	cmd.Flags().StringVarP(&summary, "message", "m", "", "what changed (required)")
	cmd.Flags().StringVar(&verified, "verified", "", "how it was verified (required for agents)")
	cmd.Flags().StringArrayVar(&followups, "followup", nil, "remaining work or known gaps (repeatable)")
	cmd.Flags().StringSliceVarP(&files, "file", "f", nil, "extra files to count in the diff stats")
	return cmd
}

func (a *app) doneCmd() *cobra.Command {
	var note, approver string
	cmd := &cobra.Command{
		Use:   "done <id>",
		Short: "Mark an item done (tickets: agents when ready; stories/epics: humans)",
		Long: `Mark an item done.

Tickets: an agent may close a ticket once it has been submitted for review and
every deliverable and check is resolved. No human approval is required. Pass
--approved-by to record a human who asked for the close.

Stories and epics: only a human can close these after review. Agents cannot
close them, even with --approved-by.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.load(); err != nil {
				return err
			}
			it, err := a.store.Get(args[0])
			if err != nil {
				return err
			}
			if err := workflow.CheckDone(a.actor, it); err != nil {
				return err
			}
			from := it.Status
			it.Status = model.Done
			if err := a.store.Save(it); err != nil {
				return err
			}
			if err := a.record(journal.Event{Item: it.ID, Type: "status", From: string(from), To: string(model.Done),
				Message: note, ApprovedBy: approver, Commit: gitinfo.Head(a.store.CodeRoot)}); err != nil {
				return err
			}
			return a.done(it)
		},
	}
	cmd.Flags().StringVarP(&note, "message", "m", "", "note for the worklog")
	cmd.Flags().StringVar(&approver, "approved-by", "", "optional human to record as the approver")
	return cmd
}

func (a *app) reviewCmd() *cobra.Command {
	var notes string
	var pass, reject, spawn bool
	cmd := &cobra.Command{
		Use:   "review <id> (--pass | --reject) -m \"notes\"",
		Short: "Review work in agent review: pass it to human review, or send it back",
		Long: `Review an item in agent_review, where agent submissions land. A human or
another agent checks the work, then:

  --pass    moves it to review for a human look, or straight to done
            when .sherpa/policy.yaml allows it (tickets only; see shp policy)
  --reject  sends it back to in_progress with what needs fixing

  --spawn   launches the project's reviewer agent (reviewer.command in
            .sherpa/config.yaml) to do the review in its own session

Notes (-m) are required and go in the worklog. The session that submitted
the work can't review it. To close a ticket outright, use shp done once its
deliverables and checks are resolved; stories and epics are human-only.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if spawn {
				if pass || reject {
					return errors.New("--spawn launches a reviewer; it can't be combined with a verdict")
				}
				if err := a.load(); err != nil {
					return err
				}
				it, err := a.store.Get(args[0])
				if err != nil {
					return err
				}
				if it.Status != model.AgentReview {
					return fmt.Errorf("%s is %s; reviewers are for items in agent_review", it.ID, it.Status)
				}
				return a.launchReviewer(it)
			}
			if pass == reject {
				return errors.New("give exactly one of --pass or --reject")
			}
			if strings.TrimSpace(notes) == "" {
				return errors.New("review notes are required (-m \"…\")")
			}
			if err := a.load(); err != nil {
				return err
			}
			it, err := a.store.Get(args[0])
			if err != nil {
				return err
			}
			if it.Status != model.AgentReview {
				return fmt.Errorf("%s is %s; shp review is for items in agent_review", it.ID, it.Status)
			}
			if err := a.selfReview(it.ID); err != nil {
				return err
			}
			to, verdict := model.Review, "pass"
			if reject {
				to, verdict = model.InProgress, "reject"
			}
			rule, err := a.policyClose(it, pass)
			if err != nil {
				return err
			}
			var from model.Status
			if rule != "" { // the project's policy closes it; no human gate
				from, it.Status, to = it.Status, model.Done, model.Done
				err = a.store.Save(it)
			} else {
				from, err = a.setStatus(it, to)
			}
			if err != nil {
				return err
			}
			if err := a.record(journal.Event{Item: it.ID, Type: "review", Kind: verdict, From: string(from), To: string(to),
				Message: notes, ApprovedBy: rule, Commit: gitinfo.Head(a.store.CodeRoot)}); err != nil {
				return err
			}
			return a.done(it)
		},
	}
	cmd.Flags().StringVarP(&notes, "message", "m", "", "review notes (required)")
	cmd.Flags().BoolVar(&pass, "pass", false, "send it on to human review")
	cmd.Flags().BoolVar(&reject, "reject", false, "send it back to in progress")
	cmd.Flags().BoolVar(&spawn, "spawn", false, "launch the configured reviewer agent instead")
	return cmd
}

func (a *app) contextCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "context",
		Short: "What to work on: active ticket, in-flight work and the ready queue",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.load(); err != nil {
				return err
			}
			all, err := a.store.All()
			if err != nil {
				return err
			}
			activeID, err := a.activeID()
			if err != nil {
				return err
			}
			var active *model.Item
			var inProgress, ready, agentReview, review []*model.Item
			for _, it := range all {
				if it.ID == activeID {
					active = it
				}
				if it.Kind != model.Ticket {
					continue
				}
				switch it.Status {
				case model.InProgress:
					inProgress = append(inProgress, it)
				case model.Ready:
					ready = append(ready, it)
				case model.AgentReview:
					agentReview = append(agentReview, it)
				case model.Review:
					review = append(review, it)
				}
			}
			if a.json {
				return a.emit(map[string]any{
					"project": a.store.Config, "actor": a.actor.String(), "session": a.session,
					"active": active, "in_progress": inProgress, "ready": ready, "agent_review": agentReview, "review": review,
				})
			}
			fmt.Printf("Sherpa · %s (%s) · you are %s\n\n", a.store.Config.Name, a.store.Config.Key, a.actor)
			if active != nil {
				fmt.Println("Active:", render.Line(active))
				fmt.Printf("  brief: `shp brief %s` gives the outcome, deliverables, next check and code map.\n", active.ID)
			} else {
				fmt.Println("Active: none — claim one with `shp start <id>`")
			}
			section := func(title string, items []*model.Item) {
				if len(items) == 0 {
					return
				}
				fmt.Printf("\n%s:\n", title)
				for _, it := range items {
					fmt.Println("  " + render.Line(it))
				}
			}
			section("In progress", inProgress)
			section("Ready", ready)
			section("Awaiting agent review (shp review <id> --pass|--reject)", agentReview)
			section("Awaiting review", review)
			return nil
		},
	}
}

func (a *app) activityCmd() *cobra.Command {
	var limit int
	var item string
	cmd := &cobra.Command{
		Use:   "activity",
		Short: "Show the audit log across all sessions",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.load(); err != nil {
				return err
			}
			events, err := journal.ReadAll(a.store.Dir)
			if err != nil {
				return err
			}
			if item != "" {
				norm, _, _, err := model.ParseID(item)
				if err != nil {
					return err
				}
				var kept []journal.Event
				for _, e := range events {
					if e.Item == norm {
						kept = append(kept, e)
					}
				}
				events = kept
			}
			if limit > 0 && len(events) > limit {
				events = events[len(events)-limit:]
			}
			if a.json {
				return a.emit(events)
			}
			for _, e := range events {
				icon, _, headline, detail := worklog.Describe(e)
				if e.Type == "submit" || e.Type == "status" {
					if len(detail) > 0 {
						headline += ": " + detail[0]
					}
				}
				fmt.Printf("%s  %-7s %s %-16s %s\n", e.TS.Local().Format("01-02 15:04"), e.Item, icon, e.Actor, ansi.Strip(headline))
			}
			return nil
		},
	}
	cmd.Flags().IntVarP(&limit, "limit", "n", 30, "most recent N events (0 = all)")
	cmd.Flags().StringVarP(&item, "item", "i", "", "only events for this ID")
	return cmd
}

func (a *app) editCmd() *cobra.Command {
	var fromStdin bool
	cmd := &cobra.Command{
		Use:   "edit <id>",
		Short: "Open an item in $EDITOR, or replace its body from stdin (--stdin)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.load(); err != nil {
				return err
			}
			it, err := a.store.Get(args[0])
			if err != nil {
				return err
			}
			if fromStdin {
				data, err := io.ReadAll(os.Stdin)
				if err != nil {
					return err
				}
				if strings.TrimSpace(string(data)) == "" {
					return errors.New("refusing to replace the body with empty stdin")
				}
				it.Body = string(data)
				if p := model.GoalProblem(it); p != "" {
					return errors.New(p)
				}
				if err := a.store.Save(it); err != nil {
					return err
				}
				if err := a.record(journal.Event{Item: it.ID, Type: "edit", Message: "body replaced from stdin"}); err != nil {
					return err
				}
				return a.done(it)
			}
			editor := os.Getenv("EDITOR")
			if editor == "" {
				editor = "vi"
			}
			path := filepath.Join(a.store.Dir, it.Kind.Dir(), it.ID+".md")
			c := exec.Command("sh", "-c", editor+` "$1"`, "sh", path)
			c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
			if err := c.Run(); err != nil {
				return err
			}
			edited, err := a.store.Get(it.ID)
			if err != nil {
				return fmt.Errorf("file no longer parses, fix it before continuing: %w", err)
			}
			if err := a.store.Save(edited); err != nil {
				return err
			}
			if err := a.record(journal.Event{Item: it.ID, Type: "edit"}); err != nil {
				return err
			}
			if p := model.GoalProblem(edited); p != "" {
				return errors.New(p + " · saved anyway, run shp edit " + it.ID + " again to fix it")
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&fromStdin, "stdin", false, "replace the body with markdown read from stdin")
	return cmd
}

// done prints the updated item after a mutating command.
func (a *app) done(it *model.Item) error {
	if a.json {
		return a.emit(it)
	}
	fmt.Println(render.Line(it))
	return nil
}

func (a *app) migrateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Upgrade .sherpa data to the current format (idempotent)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.load(); err != nil {
				return err
			}
			items, err := a.store.All()
			if err != nil {
				return err
			}
			n := 0
			for _, it := range items {
				body, changed := store.StripWorklog(it.Body)
				if !changed {
					continue
				}
				it.Body = body
				if err := a.store.Save(it); err != nil {
					return err
				}
				if err := a.record(journal.Event{Item: it.ID, Type: "migrate", Message: "moved worklog out of the body (now generated from the journal)"}); err != nil {
					return err
				}
				n++
			}
			fmt.Printf("Moved worklogs out of %d item bodies; worklogs are now generated from .sherpa/sessions.\n", n)
			return nil
		},
	}
	cmd.AddCommand(a.migrateBriefCmd())
	return cmd
}

func (a *app) importCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "import", Short: "Import issues from another tracker"}
	var source, mappingPath string
	agency := &cobra.Command{
		Use:   "agency --mapping <file.yaml>",
		Short: "Import Agency issues (.agency/issues) into epics, stories and tickets",
		Long: `Import Agency issues using a YAML mapping that places every issue in a story:

  key: APP
  epics:
    - title: "Platform"
      goal: "…"
      stories:
        - title: "Auth"
          description: "…"
          issues: [1, 2, 3]

Ticket numbers match issue numbers (APP-30 → T-0030). todo becomes ready when
all its dependencies are done, otherwise backlog. Comments become worklog
entries; the original files are archived in .sherpa/imports/agency/.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.load(); err != nil {
				return err
			}
			data, err := os.ReadFile(mappingPath)
			if err != nil {
				return err
			}
			var m importer.Mapping
			if err := yaml.Unmarshal(data, &m); err != nil {
				return fmt.Errorf("mapping: %w", err)
			}
			if source == "" {
				source = filepath.Join(a.store.Root, ".agency", "issues")
			}
			res, err := importer.Agency(a.store, source, &m)
			if err != nil {
				return err
			}
			if err := os.WriteFile(filepath.Join(a.store.Dir, "imports", "agency", "mapping.yaml"), data, 0o644); err != nil {
				return err
			}
			if a.json {
				return a.emit(res)
			}
			fmt.Printf("Imported %d epics, %d stories, %d tickets (%d journal events)\n", res.Epics, res.Stories, res.Tickets, res.Events)
			for _, st := range model.Statuses {
				if n := res.ByStatus[st]; n > 0 {
					fmt.Printf("  %s %-12s %d\n", st.Glyph(), st, n)
				}
			}
			return nil
		},
	}
	agency.Flags().StringVar(&mappingPath, "mapping", "", "YAML mapping of epics/stories to issue numbers (required)")
	agency.Flags().StringVar(&source, "source", "", "issues directory (default .agency/issues)")
	_ = agency.MarkFlagRequired("mapping")
	cmd.AddCommand(agency)
	return cmd
}

// linkWork attaches the PR for the current branch and the commits that name
// the item, when they can be found. Missing gh or no PR is not an error.
func (a *app) linkWork(it *model.Item) {
	if it.Links == nil {
		it.Links = &model.Links{}
	}
	l := it.Links
	if links.Available() {
		ref := l.PR // refresh a known PR, else look one up for this branch
		if ref != "" {
			_ = links.View(a.store.CodeRoot, ref, l)
		} else if br := gitinfo.Branch(a.store.CodeRoot); br != "" {
			_ = links.ForBranch(a.store.CodeRoot, l)
		}
	}
	if len(l.Commits) == 0 {
		l.Commits = links.TrailerCommits(a.store.CodeRoot, it.ID)
	}
	if l.Empty() {
		it.Links = nil
	}
}

// reviewWarnings lists what a reviewer should know is missing. They are
// warnings only: submit never blocks on them.
func reviewWarnings(it *model.Item) []string {
	var w []string
	if it.Links == nil || it.Links.PR == "" {
		w = append(w, "⚠ no PR linked: open one with `gh pr create` (Sherpa links it), or run `shp sync` after it exists")
	}
	var open, hand []string
	for i, c := range it.Checks {
		switch {
		case c.Resolved():
		case c.Manual: // not the agent's to tick: a human verifies these
			hand = append(hand, fmt.Sprintf("%d %s", i+1, c.Text))
		default:
			open = append(open, fmt.Sprintf("%d %s", i+1, c.Text))
		}
	}
	if len(open) > 0 {
		r, t := it.CheckCounts()
		w = append(w, fmt.Sprintf("⚠ checks %d/%d: unresolved %s", r, t, strings.Join(open, "; ")))
	}
	if len(hand) > 0 {
		w = append(w, "✋ for a human to verify: "+strings.Join(hand, "; "))
	}
	if len(it.Deliverables) == 0 {
		if it.Template != "spike" {
			w = append(w, "⚠ no deliverables: state the outcomes with `shp deliverable add …`")
		}
	} else {
		for i := range it.Deliverables {
			if len(it.ChecksFor(i+1)) == 0 {
				w = append(w, fmt.Sprintf("⚠ deliverable %d has no check: `shp check add --for %d …`", i+1, i+1))
			}
		}
	}
	for i, c := range it.Checks {
		if c.For < 0 || c.For > len(it.Deliverables) {
			w = append(w, fmt.Sprintf("⚠ check %d points at deliverable %d, but there are %d", i+1, c.For, len(it.Deliverables)))
		}
	}
	if it.Template != "spike" && len(it.Code) == 0 {
		w = append(w, "⚠ no code map: `shp where add <path> --symbols … --deliverable N`")
	}
	for _, m := range it.Measures {
		if met, known := m.Met(); known && !met {
			w = append(w, fmt.Sprintf("⚠ measure %q at %s, target %s", m.Name, m.Current, m.Target))
		}
	}
	if it.Now == "" {
		w = append(w, "⚠ no Now line: `shp now \"…\"` tells reviewers where things stand")
	}
	return w
}

func (a *app) syncCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "sync [id...]",
		Short: "Link PRs and commits to tickets and refresh PR state (needs gh)",
		Long: `For each ticket (default: every ticket in progress, review or done):
refresh its linked PR, or find the PR that delivered it by branch name or
title (T-0030, or an imported key like app-30), plus commits whose message
has "Ticket: <id>".`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.load(); err != nil {
				return err
			}
			if !links.Available() {
				return errors.New("shp sync needs the GitHub CLI (gh)")
			}
			var items []*model.Item
			if len(args) > 0 {
				for _, id := range args {
					it, err := a.store.Get(id)
					if err != nil {
						return err
					}
					items = append(items, it)
				}
			} else {
				all, err := a.store.All()
				if err != nil {
					return err
				}
				for _, it := range all {
					if it.Kind == model.Ticket && (it.Status == model.InProgress || it.Status == model.AgentReview || it.Status == model.Review || it.Status == model.Done) {
						items = append(items, it)
					}
				}
			}
			prs, err := links.List(a.store.CodeRoot)
			if err != nil {
				return err
			}
			linked, refreshed := 0, 0
			for _, it := range items {
				before, _ := json.Marshal(it.Links)
				l := it.Links
				if l == nil {
					l = &model.Links{}
				}
				isNew := l.PR == ""
				switch {
				case !isNew:
					_ = links.View(a.store.CodeRoot, l.PR, l)
				default:
					if all := links.MatchAll(prs, links.Keys(it)); len(all) > 0 {
						_ = links.ApplyMatch(a.store.CodeRoot, all[0], l)
						for _, p := range all[1:] {
							if !slices.Contains(l.Related, p.URL) {
								l.Related = append(l.Related, p.URL)
							}
						}
					}
				}
				for _, c := range links.TrailerCommits(a.store.CodeRoot, it.ID) {
					if !slices.ContainsFunc(l.Commits, func(x model.Commit) bool { return x.SHA == c.SHA }) {
						l.Commits = append(l.Commits, c)
					}
				}
				if l.Empty() {
					continue
				}
				it.Links = l
				after, _ := json.Marshal(it.Links)
				if string(before) == string(after) {
					continue
				}
				if err := a.store.Write(it); err != nil { // keep Updated: sync isn't an edit
					return err
				}
				if isNew && l.PR != "" {
					linked++
					_ = a.record(journal.Event{Item: it.ID, Type: "link", Message: fmt.Sprintf("linked PR #%d (%s)", l.PRNumber, l.PRState), Detail: []string{l.PR}})
					if !a.json {
						fmt.Printf("⇡ %s ← PR #%d %s\n", it.ID, l.PRNumber, l.PRState)
					}
				} else {
					refreshed++
				}
			}
			if a.json {
				return a.emit(map[string]int{"linked": linked, "refreshed": refreshed, "checked": len(items)})
			}
			fmt.Printf("Checked %d tickets: %d PRs newly linked, %d refreshed\n", len(items), linked, refreshed)
			return nil
		},
	}
}
