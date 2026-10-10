// Package cli wires Sherpa's commands. Every command supports --json so
// agents can drive it as reliably as humans.
package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/RL-Charles/sherpa/internal/gitinfo"
	"github.com/RL-Charles/sherpa/internal/journal"
	"github.com/RL-Charles/sherpa/internal/live"
	"github.com/RL-Charles/sherpa/internal/model"
	"github.com/RL-Charles/sherpa/internal/render"
	"github.com/RL-Charles/sherpa/internal/store"
	"github.com/RL-Charles/sherpa/internal/tui"
	"github.com/RL-Charles/sherpa/internal/workflow"
)

var Version = "0.1.0-dev"

type app struct {
	json    bool
	store   *store.Store
	actor   workflow.Actor
	session string
}

func Execute() error {
	a := &app{}
	root := &cobra.Command{
		Use:           CommandName(os.Args[0]),
		Short:         "Sherpa — tickets and progress tracking for humans and agents",
		Long:          "Sherpa — tickets and progress tracking for humans and agents.\n\nRun with no arguments to open the TUI. The same program is installed as shp (short, what agents use) and sherpa.",
		Version:       Version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.Args = cobra.NoArgs
	root.RunE = func(cmd *cobra.Command, args []string) error {
		if a.json || !render.IsTTY() {
			return cmd.Help()
		}
		if err := a.load(); err != nil {
			return err
		}
		return tui.Run(a.store)
	}
	root.PersistentFlags().BoolVar(&a.json, "json", false, "machine-readable output")
	root.AddCommand(
		a.initCmd(),
		a.newCmd(),
		a.lsCmd(),
		a.showCmd(),
		a.statusCmd(),
		a.startCmd(),
		a.logCmd(),
		a.submitCmd(),
		a.doneCmd(),
		a.reviewCmd(),
		a.contextCmd(),
		a.activityCmd(),
		a.editCmd(),
		a.migrateCmd(),
		a.importCmd(),
		a.hookCmd(),
		a.hooksCmd(),
		a.nowCmd(),
		a.checkCmd(),
		a.checksCmd(),
		a.measureCmd(),
		a.visualCmd(),
		a.dependsCmd(),
		a.syncCmd(),
		a.backfillCmd(),
	)
	err := root.Execute()
	if err != nil {
		if a.json {
			_ = json.NewEncoder(os.Stderr).Encode(map[string]string{"error": err.Error()})
		} else {
			fmt.Fprintln(os.Stderr, CommandName(os.Args[0])+":", err)
		}
	}
	return err
}

// load opens the store from the working directory and identifies the actor.
func (a *app) load() error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	s, err := store.Find(cwd)
	if err != nil {
		return err
	}
	a.store = s
	a.actor = workflow.CurrentActor()
	a.session = journal.Session(a.actor.String())
	if os.Getenv("SHERPA_SESSION") == "" && s.CodeRoot != s.Root {
		// Parallel agents in different worktrees each get their own session,
		// so they never overwrite one another's active ticket.
		a.session += "-" + filepath.Base(s.CodeRoot)
	}
	return nil
}

func (a *app) record(e journal.Event) error {
	e.Actor = a.actor.String()
	e.Session = a.session
	if a.actor.Kind == workflow.Agent { // the TUI's live "working" indicator
		switch e.Type {
		case "claim", "log":
			_ = live.Touch(a.store, a.actor.Name, e.Item)
		case "submit":
			_ = live.Clear(a.store)
		}
	}
	return journal.Append(a.store.Dir, e)
}

func (a *app) emit(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// The active ticket is tracked per checkout (main or worktree) by the store.
func (a *app) activeID() (string, error) { return a.store.Active() }

func (a *app) setActive(id string) error { return a.store.SetActive(id) }

// resolve returns the item named by args[0], or the session's active ticket.
func (a *app) resolve(args []string) (*model.Item, error) {
	if len(args) > 0 && args[0] != "" {
		return a.store.Get(args[0])
	}
	id, err := a.activeID()
	if err != nil {
		return nil, err
	}
	if id == "" {
		return nil, errors.New("no ID given and no active ticket (run `shp start <id>`)")
	}
	return a.store.Get(id)
}

// setStatus checks the workflow and saves the new status, returning the old one.
func (a *app) setStatus(it *model.Item, to model.Status) (model.Status, error) {
	if err := workflow.CheckTransition(a.actor, it, to); err != nil {
		return "", err
	}
	from := it.Status
	it.Status = to
	return from, a.store.Save(it)
}

// transition changes status and records it in the journal.
func (a *app) transition(it *model.Item, to model.Status, note string) error {
	from, err := a.setStatus(it, to)
	if err != nil {
		return err
	}
	return a.record(journal.Event{Item: it.ID, Type: "status", From: string(from), To: string(to), Message: note,
		Commit: gitinfo.Head(a.store.CodeRoot)})
}

// workSession returns the commit the item's current work session started
// from and every file logged in it since, from the journal.
func (a *app) workSession(id string) (base string, files []string, err error) {
	events, err := journal.ReadAll(a.store.Dir)
	if err != nil {
		return "", nil, err
	}
	seen := map[string]bool{}
	for _, e := range journal.ForItem(events, id) {
		if e.Type == "claim" {
			base, files, seen = e.Commit, nil, map[string]bool{}
		}
		for _, f := range e.Files {
			if !seen[f] {
				seen[f] = true
				files = append(files, f)
			}
		}
	}
	return base, files, nil
}

// CommandName is how Sherpa refers to itself in help and errors: "sherpa"
// when run under that name, otherwise "shp".
func CommandName(argv0 string) string {
	base := strings.TrimSuffix(filepath.Base(argv0), ".exe")
	if base == "sherpa" {
		return "sherpa"
	}
	return "shp"
}
