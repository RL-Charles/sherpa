package cli

import (
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

	"github.com/RL-Charles/sherpa/internal/gitinfo"
	"github.com/RL-Charles/sherpa/internal/journal"
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
			it, err := a.store.Create(n)
			if err != nil {
				return err
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
			fmt.Print(render.Markdown(render.Document(it, parent, store.Children(all, it.ID))))
			width := 100
			if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 20 {
				width = min(w, 120)
			}
			fmt.Println()
			for _, l := range worklog.Render(sessions, width, expandAll) {
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
				Commit: gitinfo.Head(a.store.Root)}); err != nil {
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
			added, removed := gitinfo.DiffStat(a.store.Root, base, files)
			e := journal.Event{Item: it.ID, Type: "log", Kind: kind, Message: strings.Join(args, " "),
				Detail: detail, Files: files, Commit: gitinfo.Head(a.store.Root), Added: added, Removed: removed}
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
			from, err := a.setStatus(it, model.Review)
			if err != nil {
				return err
			}
			added, removed := gitinfo.DiffStat(a.store.Root, base, sessionFiles)
			if err := a.record(journal.Event{Item: it.ID, Type: "submit", From: string(from), To: string(model.Review),
				Message: summary, Verified: verified, Followups: followups, Files: sessionFiles,
				Commit: gitinfo.Head(a.store.Root), Added: added, Removed: removed}); err != nil {
				return err
			}
			if active, _ := a.activeID(); active == it.ID {
				if err := a.setActive(""); err != nil {
					return err
				}
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
	var note string
	cmd := &cobra.Command{
		Use:   "done <id>",
		Short: "Mark an item done (humans only)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.load(); err != nil {
				return err
			}
			it, err := a.store.Get(args[0])
			if err != nil {
				return err
			}
			if err := a.transition(it, model.Done, note); err != nil {
				return err
			}
			return a.done(it)
		},
	}
	cmd.Flags().StringVarP(&note, "message", "m", "", "note for the worklog")
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
			var inProgress, ready, review []*model.Item
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
				case model.Review:
					review = append(review, it)
				}
			}
			if a.json {
				return a.emit(map[string]any{
					"project": a.store.Config, "actor": a.actor.String(), "session": a.session,
					"active": active, "in_progress": inProgress, "ready": ready, "review": review,
				})
			}
			fmt.Printf("Sherpa · %s (%s) · you are %s\n\n", a.store.Config.Name, a.store.Config.Key, a.actor)
			if active != nil {
				fmt.Println("Active:", render.Line(active))
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
			return a.record(journal.Event{Item: it.ID, Type: "edit"})
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
	return &cobra.Command{
		Use:    "migrate",
		Short:  "Upgrade .sherpa data to the current format (idempotent)",
		Hidden: true,
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
}
