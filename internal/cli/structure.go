package cli

import (
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/RL-Charles/sherpa/internal/journal"
	"github.com/RL-Charles/sherpa/internal/model"
	"github.com/RL-Charles/sherpa/internal/render"
)

// Deliverables are outcomes ("what exists when this is done"), separate from
// checks ("how we prove it"). They live in frontmatter so agents update them
// with commands and the ticket stays readable.

func (a *app) deliverableCmd() *cobra.Command {
	var id string
	cmd := &cobra.Command{
		Use:     "deliverable",
		Aliases: []string{"deliverables"},
		Short:   "Manage a ticket's deliverables (outcomes)",
		Long: `Deliverables are the outcomes a ticket promises: what exists after it is
done. Checks prove each outcome.

  shp deliverable add "Refresh accepts an expired access token"
  shp deliverable done 1 -e internal/auth/token_test.go:TestRotateOnUse
  shp deliverable list
  shp check add --for 1 "unit: go test ./internal/auth -run Rotate"`,
	}
	cmd.PersistentFlags().StringVarP(&id, "ticket", "t", "", "item (default: active ticket)")

	add := &cobra.Command{
		Use:   "add <outcome...>",
		Short: "Add a deliverable",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.load(); err != nil {
				return err
			}
			it, err := a.resolve([]string{id})
			if err != nil {
				return err
			}
			text := strings.TrimSpace(strings.Join(args, " "))
			it.Deliverables = append(it.Deliverables, model.Deliverable{Text: text})
			if err := a.store.Save(it); err != nil {
				return err
			}
			n := len(it.Deliverables)
			if a.json {
				return a.emit(it)
			}
			fmt.Printf("%s deliverable %d: %s\n", it.ID, n, text)
			return a.record(journal.Event{Item: it.ID, Type: "deliverable", Message: fmt.Sprintf("added deliverable %d: %s", n, text)})
		},
	}

	var evidence string
	done := &cobra.Command{
		Use:   "done <n>",
		Short: "Mark deliverable n as delivered",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.load(); err != nil {
				return err
			}
			it, err := a.resolve([]string{id})
			if err != nil {
				return err
			}
			n, err := deliverableIndex(it, args[0])
			if err != nil {
				return err
			}
			it.Deliverables[n-1].Done = true
			if evidence != "" {
				it.Deliverables[n-1].Evidence = evidence
			}
			msg := fmt.Sprintf("deliverable %d done: %s", n, it.Deliverables[n-1].Text)
			var detail []string
			if it.Deliverables[n-1].Evidence != "" {
				detail = []string{"evidence: " + it.Deliverables[n-1].Evidence}
			}
			return a.saveAndRecord(it, journal.Event{Type: "deliverable", Message: msg, Detail: detail})
		},
	}
	done.Flags().StringVarP(&evidence, "evidence", "e", "", "commit, PR, path or test proving the outcome")

	undo := &cobra.Command{
		Use:   "undo <n>",
		Short: "Mark deliverable n as not delivered",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.load(); err != nil {
				return err
			}
			it, err := a.resolve([]string{id})
			if err != nil {
				return err
			}
			n, err := deliverableIndex(it, args[0])
			if err != nil {
				return err
			}
			it.Deliverables[n-1].Done = false
			it.Deliverables[n-1].Evidence = ""
			return a.saveAndRecord(it, journal.Event{Type: "deliverable", Message: fmt.Sprintf("deliverable %d reopened", n)})
		},
	}

	rm := &cobra.Command{
		Use:   "rm <n>",
		Short: "Remove deliverable n and unlink its checks and code entries",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.load(); err != nil {
				return err
			}
			it, err := a.resolve([]string{id})
			if err != nil {
				return err
			}
			n, err := deliverableIndex(it, args[0])
			if err != nil {
				return err
			}
			it.Deliverables = append(it.Deliverables[:n-1], it.Deliverables[n:]...)
			reindexDeliverableLinks(it, n)
			return a.saveAndRecord(it, journal.Event{Type: "deliverable", Message: fmt.Sprintf("removed deliverable %d", n)})
		},
	}

	list := &cobra.Command{
		Use:   "list [id]",
		Short: "List deliverables and their checks",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.load(); err != nil {
				return err
			}
			sel := []string{id}
			if len(args) == 1 {
				sel = args
			}
			it, err := a.resolve(sel)
			if err != nil {
				return err
			}
			if a.json {
				return a.emit(it.Deliverables)
			}
			done, total := it.DeliverableCounts()
			fmt.Printf("%s deliverables %d/%d\n", it.ID, done, total)
			for i, d := range it.Deliverables {
				mark := "☐"
				if d.Done {
					mark = "☑"
				}
				fmt.Printf("  %d %s %s\n", i+1, mark, d.Text)
				if d.Evidence != "" {
					fmt.Println("      evidence: " + d.Evidence)
				}
				if d.Note != "" {
					fmt.Println("      note: " + d.Note)
				}
				r, t := it.CheckCountsFor(i + 1)
				fmt.Printf("      checks: %d/%d\n", r, t)
			}
			return nil
		},
	}

	cmd.AddCommand(add, done, undo, rm, list)
	return cmd
}

func deliverableIndex(it *model.Item, arg string) (int, error) {
	if len(it.Deliverables) == 0 {
		return 0, fmt.Errorf("%s has no deliverables yet; add one with `shp deliverable add \"outcome\"`", it.ID)
	}
	n, err := strconv.Atoi(arg)
	if err != nil || n < 1 || n > len(it.Deliverables) {
		return 0, fmt.Errorf("%s has %d deliverables; pick 1–%d", it.ID, len(it.Deliverables), len(it.Deliverables))
	}
	return n, nil
}

// reindexDeliverableLinks drops links to the removed deliverable and shifts
// later links down, so a removal never silently points at the wrong outcome.
func reindexDeliverableLinks(it *model.Item, removed int) {
	for i := range it.Checks {
		switch {
		case it.Checks[i].For == removed:
			it.Checks[i].For = 0
		case it.Checks[i].For > removed:
			it.Checks[i].For--
		}
	}
	for i := range it.Code {
		switch {
		case it.Code[i].Deliverable == removed:
			it.Code[i].Deliverable = 0
		case it.Code[i].Deliverable > removed:
			it.Code[i].Deliverable--
		}
	}
}

// Where is the planned code map: which files and symbols the work touches,
// what kind of change each is, and which deliverable it serves.

func (a *app) whereCmd() *cobra.Command {
	var id string
	cmd := &cobra.Command{
		Use:   "where",
		Short: "Manage a ticket's code map (files, symbols, change type)",
		Long: `Where records the files and symbols a ticket changes, before the diff.

  shp where add internal/auth/token.go --symbols Rotate,Clock --deliverable 1
  shp where add web/src/session.ts --change modify --symbols retryOnce --deliverable 3
  shp where state internal/auth/token.go done
  shp where list
  shp where rm web/src/session.ts`,
	}
	cmd.PersistentFlags().StringVarP(&id, "ticket", "t", "", "item (default: active ticket)")

	var change, symbols, note, lines, status string
	var deliverable int
	add := &cobra.Command{
		Use:   "add <path>",
		Short: "Add or update a file in the code map",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.load(); err != nil {
				return err
			}
			it, err := a.resolve([]string{id})
			if err != nil {
				return err
			}
			ref, err := makeCodeRef(it, args[0], change, symbols, note, lines, status, deliverable)
			if err != nil {
				return err
			}
			idx := -1
			for i, c := range it.Code {
				if samePath(c.Path, ref.Path) {
					idx = i
					break
				}
			}
			if idx >= 0 {
				old := it.Code[idx]
				if !cmd.Flags().Changed("change") && old.Change != "" {
					ref.Change = old.Change
				}
				if !cmd.Flags().Changed("status") && old.Status != "" {
					ref.Status = old.Status
				}
				if !cmd.Flags().Changed("symbols") {
					ref.Symbols = old.Symbols
				}
				if !cmd.Flags().Changed("note") {
					ref.Note = old.Note
				}
				if !cmd.Flags().Changed("lines") {
					ref.Lines = old.Lines
				}
				if !cmd.Flags().Changed("deliverable") {
					ref.Deliverable = old.Deliverable
				}
				it.Code[idx] = ref
			} else {
				it.Code = append(it.Code, ref)
			}
			msg := fmt.Sprintf("where: %s (%s)", ref.Path, ref.Change)
			return a.saveAndRecord(it, journal.Event{Type: "where", Message: msg, Detail: codeDetail(ref)})
		},
	}
	add.Flags().StringVar(&change, "change", "modify", "add, modify, remove or move")
	add.Flags().StringVar(&symbols, "symbols", "", "comma-separated functions, types or patterns")
	add.Flags().StringVar(&note, "note", "", "why this file changes")
	add.Flags().StringVar(&lines, "lines", "", "line range, e.g. 40-88")
	add.Flags().StringVar(&status, "status", "planned", "planned, touched or done")
	add.Flags().IntVar(&deliverable, "deliverable", 0, "1-based deliverable this serves")

	var rmPath string
	rm := &cobra.Command{
		Use:   "rm <path>",
		Short: "Remove a file from the code map",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.load(); err != nil {
				return err
			}
			it, err := a.resolve([]string{id})
			if err != nil {
				return err
			}
			idx := -1
			for i, c := range it.Code {
				if samePath(c.Path, args[0]) {
					idx = i
					break
				}
			}
			if idx < 0 {
				return fmt.Errorf("%s has no code entry for %q", it.ID, args[0])
			}
			rmPath = it.Code[idx].Path
			it.Code = append(it.Code[:idx], it.Code[idx+1:]...)
			return a.saveAndRecord(it, journal.Event{Type: "where", Message: "where: removed " + rmPath})
		},
	}

	var stateCmd = &cobra.Command{
		Use:   "state <path> <planned|touched|done>",
		Short: "Set the state of a code-map entry",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.load(); err != nil {
				return err
			}
			it, err := a.resolve([]string{id})
			if err != nil {
				return err
			}
			st, err := normalizeCodeState(args[1])
			if err != nil {
				return err
			}
			for i := range it.Code {
				if samePath(it.Code[i].Path, args[0]) {
					it.Code[i].Status = st
					return a.saveAndRecord(it, journal.Event{Type: "where", Message: fmt.Sprintf("where: %s is %s", it.Code[i].Path, st)})
				}
			}
			return fmt.Errorf("%s has no code entry for %q", it.ID, args[0])
		},
	}

	list := &cobra.Command{
		Use:   "list [id]",
		Short: "Show the code map",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.load(); err != nil {
				return err
			}
			sel := []string{id}
			if len(args) == 1 {
				sel = args
			}
			it, err := a.resolve(sel)
			if err != nil {
				return err
			}
			if a.json {
				return a.emit(it.Code)
			}
			if len(it.Code) == 0 {
				fmt.Printf("%s has no code map yet. Add one with `shp where add <path>`.\n", it.ID)
				return nil
			}
			fmt.Printf("%s where · %d files\n\n%s\n", it.ID, len(it.Code), render.CodeMap(it))
			return nil
		},
	}

	cmd.AddCommand(add, rm, stateCmd, list)
	return cmd
}

func makeCodeRef(it *model.Item, p, change, symbols, note, lines, status string, deliverable int) (model.CodeRef, error) {
	p = filepath.ToSlash(filepath.Clean(strings.TrimSpace(p)))
	if p == "" || p == "." {
		return model.CodeRef{}, errors.New("a path is required")
	}
	ch, err := normalizeCodeChange(change)
	if err != nil {
		return model.CodeRef{}, err
	}
	st, err := normalizeCodeState(status)
	if err != nil {
		return model.CodeRef{}, err
	}
	if deliverable < 0 || deliverable > len(it.Deliverables) {
		if len(it.Deliverables) == 0 && deliverable > 0 {
			return model.CodeRef{}, fmt.Errorf("%s has no deliverables yet; add one with `shp deliverable add \"outcome\"`", it.ID)
		}
		return model.CodeRef{}, fmt.Errorf("%s has %d deliverables; --deliverable must be 1–%d", it.ID, len(it.Deliverables), len(it.Deliverables))
	}
	var syms []string
	for _, s := range strings.Split(symbols, ",") {
		if s = strings.TrimSpace(s); s != "" {
			syms = append(syms, s)
		}
	}
	return model.CodeRef{Path: p, Change: ch, Symbols: syms, Note: strings.TrimSpace(note), Lines: strings.TrimSpace(lines), Deliverable: deliverable, Status: st}, nil
}

func normalizeCodeChange(s string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "modify", "modified", "change", "update":
		return "modify", nil
	case "add", "added", "new", "create":
		return "add", nil
	case "remove", "removed", "delete", "deleted":
		return "remove", nil
	case "move", "moved", "rename", "renamed":
		return "move", nil
	}
	return "", fmt.Errorf("unknown change %q (want add, modify, remove or move)", s)
}

func normalizeCodeState(s string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "planned", "plan":
		return "planned", nil
	case "touched", "touch", "started", "in_progress":
		return "touched", nil
	case "done", "complete", "completed":
		return "done", nil
	}
	return "", fmt.Errorf("unknown state %q (want planned, touched or done)", s)
}

func samePath(a, b string) bool {
	return filepath.ToSlash(filepath.Clean(a)) == filepath.ToSlash(filepath.Clean(b))
}

func codeDetail(r model.CodeRef) []string {
	var d []string
	if len(r.Symbols) > 0 {
		d = append(d, "symbols: "+strings.Join(r.Symbols, ", "))
	}
	if r.Deliverable > 0 {
		d = append(d, fmt.Sprintf("deliverable: %d", r.Deliverable))
	}
	if r.Note != "" {
		d = append(d, "note: "+r.Note)
	}
	return d
}
