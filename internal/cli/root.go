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
		Use:           "shp",
		Short:         "Sherpa — tickets and progress tracking for humans and agents",
		Long:          "Sherpa — tickets and progress tracking for humans and agents.\n\nRun with no arguments to open the TUI.",
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
		a.contextCmd(),
		a.activityCmd(),
		a.editCmd(),
		a.migrateCmd(),
	)
	err := root.Execute()
	if err != nil {
		if a.json {
			_ = json.NewEncoder(os.Stderr).Encode(map[string]string{"error": err.Error()})
		} else {
			fmt.Fprintln(os.Stderr, "shp:", err)
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
	return nil
}

func (a *app) record(e journal.Event) error {
	e.Actor = a.actor.String()
	e.Session = a.session
	return journal.Append(a.store.Dir, e)
}

func (a *app) emit(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// Active ticket per session lives in .sherpa/.local/active/<session> (gitignored).
func (a *app) activePath() string {
	return filepath.Join(a.store.Dir, ".local", "active", a.session)
}

func (a *app) activeID() (string, error) {
	data, err := os.ReadFile(a.activePath())
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	return strings.TrimSpace(string(data)), err
}

func (a *app) setActive(id string) error {
	if id == "" {
		err := os.Remove(a.activePath())
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if err := os.MkdirAll(filepath.Dir(a.activePath()), 0o755); err != nil {
		return err
	}
	return os.WriteFile(a.activePath(), []byte(id+"\n"), 0o644)
}

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
		Commit: gitinfo.Head(a.store.Root)})
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
