package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/RL-Charles/sherpa/internal/journal"
	"github.com/RL-Charles/sherpa/internal/model"
	"github.com/RL-Charles/sherpa/internal/render"
	"github.com/RL-Charles/sherpa/internal/workflow"
)

// Commands that maintain a ticket's structured sections. Each takes
// -t/--ticket and defaults to the active ticket.

func (a *app) itemFlag(cmd *cobra.Command, id *string) {
	cmd.Flags().StringVarP(id, "ticket", "t", "", "item (default: active ticket)")
}

func (a *app) saveAndRecord(it *model.Item, e journal.Event) error {
	if err := a.store.Save(it); err != nil {
		return err
	}
	e.Item = it.ID
	if err := a.record(e); err != nil {
		return err
	}
	return a.done(it)
}

func (a *app) nowCmd() *cobra.Command {
	var id string
	cmd := &cobra.Command{
		Use:   "now <one line>",
		Short: "Set the one-line current state shown at the top of the ticket",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.load(); err != nil {
				return err
			}
			it, err := a.resolve([]string{id})
			if err != nil {
				return err
			}
			it.Now = strings.TrimSpace(strings.Join(args, " "))
			if err := a.store.Save(it); err != nil {
				return err
			}
			return a.done(it)
		},
	}
	a.itemFlag(cmd, &id)
	return cmd
}

func (a *app) checkCmd() *cobra.Command {
	var id, evidence, waive, how string
	var undo, manual bool
	cmd := &cobra.Command{
		Use:   "check <n>",
		Short: "Tick acceptance check n (see `shp checks`), with evidence",
		Long: `Tick, untick or waive an acceptance check.

  shp check 2 --evidence a1b2c3d          tick check 2, citing a commit
  shp check 3 --evidence build/test/log   …or a path, URL or PR
  shp check 1 --undo                      untick
  shp check 4 --waive "covered by T-0031" resolve without doing it
  shp check 5 --manual --how "open the TUI, collapse E-001"
                                          a human must verify it by hand
  shp check add "make check passes"       add a check
  shp check add --manual "looks right at 80 cols" --how "resize tmux to 80"

Manual checks are signed off by humans only: agents can add them, but not
tick or waive them.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.load(); err != nil {
				return err
			}
			it, err := a.resolve([]string{id})
			if err != nil {
				return err
			}
			n, err := strconv.Atoi(args[0])
			if err != nil || n < 1 || n > len(it.Checks) {
				return fmt.Errorf("%s has %d checks; pick 1–%d (see `shp checks %s`)", it.ID, len(it.Checks), len(it.Checks), it.ID)
			}
			c := &it.Checks[n-1]
			msg := "check " + args[0] + ": " + c.Text
			if manual || how != "" {
				c.Manual = c.Manual || manual
				if how != "" {
					c.How = how
				}
				var detail []string
				if c.How != "" {
					detail = []string{"how: " + c.How}
				}
				return a.saveAndRecord(it, journal.Event{Type: "check", Message: "manual " + msg, Detail: detail})
			}
			if c.Manual && !undo && a.actor.Kind == workflow.Agent {
				return fmt.Errorf("check %d is manual: a human verifies it by hand (it waits for them in review)", n)
			}
			switch {
			case undo:
				c.Done, c.Waived, c.Evidence = false, "", ""
				msg = "unticked " + msg
			case waive != "":
				c.Done, c.Waived = false, waive
				msg = "waived " + msg + " (" + waive + ")"
			default:
				c.Done, c.Waived = true, ""
				if evidence != "" {
					c.Evidence = evidence
				}
			}
			var detail []string
			if c.Evidence != "" && !undo {
				detail = []string{"evidence: " + c.Evidence}
			}
			return a.saveAndRecord(it, journal.Event{Type: "check", Message: msg, Detail: detail})
		},
	}
	a.itemFlag(cmd, &id)
	cmd.Flags().StringVarP(&evidence, "evidence", "e", "", "commit, PR, path or URL proving it")
	cmd.Flags().BoolVar(&undo, "undo", false, "untick")
	cmd.Flags().StringVar(&waive, "waive", "", "resolve without doing it, with the reason")
	cmd.Flags().BoolVar(&manual, "manual", false, "mark it as needing a human to verify by hand")
	cmd.Flags().StringVar(&how, "how", "", "steps a human follows to verify a manual check")

	var addID, addHow string
	var addManual bool
	var addFor int
	add := &cobra.Command{
		Use:   "add <criterion...>",
		Short: "Add an acceptance check",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.load(); err != nil {
				return err
			}
			it, err := a.resolve([]string{addID})
			if err != nil {
				return err
			}
			if addFor < 0 || addFor > len(it.Deliverables) {
				if len(it.Deliverables) == 0 && addFor > 0 {
					return fmt.Errorf("%s has no deliverables yet; add one with `shp deliverable add \"outcome\"`", it.ID)
				}
				return fmt.Errorf("%s has %d deliverables; --for must be 1–%d", it.ID, len(it.Deliverables), len(it.Deliverables))
			}
			text := strings.TrimSpace(strings.Join(args, " "))
			it.Checks = append(it.Checks, model.Check{Text: text, Manual: addManual || addHow != "", How: addHow, For: addFor})
			if err := a.store.Save(it); err != nil {
				return err
			}
			if a.json {
				return a.emit(it)
			}
			hand := ""
			if addManual || addHow != "" {
				hand = "✋ "
			}
			link := ""
			if addFor > 0 {
				link = fmt.Sprintf(" → deliverable %d", addFor)
			}
			fmt.Printf("%s check %d: %s%s%s\n", it.ID, len(it.Checks), hand, text, link)
			return nil
		},
	}
	a.itemFlag(add, &addID)
	add.Flags().BoolVar(&addManual, "manual", false, "a human must verify it by hand")
	add.Flags().StringVar(&addHow, "how", "", "steps a human follows to verify it (implies --manual)")
	add.Flags().IntVar(&addFor, "for", 0, "1-based deliverable this check proves")

	var linkID string
	link := &cobra.Command{
		Use:   "link <n> <deliverable|0>",
		Short: "Link check n to a deliverable (0 unlinks it)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.load(); err != nil {
				return err
			}
			it, err := a.resolve([]string{linkID})
			if err != nil {
				return err
			}
			n, err := strconv.Atoi(args[0])
			if err != nil || n < 1 || n > len(it.Checks) {
				return fmt.Errorf("%s has %d checks; pick 1–%d", it.ID, len(it.Checks), len(it.Checks))
			}
			want, err := strconv.Atoi(args[1])
			if err != nil || want < 0 || want > len(it.Deliverables) {
				if len(it.Deliverables) == 0 && want > 0 {
					return fmt.Errorf("%s has no deliverables yet; add one with `shp deliverable add \"outcome\"`", it.ID)
				}
				return fmt.Errorf("%s has %d deliverables; pick 0–%d", it.ID, len(it.Deliverables), len(it.Deliverables))
			}
			it.Checks[n-1].For = want
			msg := fmt.Sprintf("check %d linked to deliverable %d", n, want)
			if want == 0 {
				msg = fmt.Sprintf("check %d unlinked", n)
			}
			return a.saveAndRecord(it, journal.Event{Type: "check", Message: msg})
		},
	}
	a.itemFlag(link, &linkID)
	cmd.AddCommand(add, link)
	return cmd
}

func (a *app) checksCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "checks [id]",
		Short: "List an item's acceptance checks",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.load(); err != nil {
				return err
			}
			it, err := a.resolve(args)
			if err != nil {
				return err
			}
			if a.json {
				return a.emit(it.Checks)
			}
			done, total := it.CheckCounts()
			fmt.Printf("%s checks %d/%d", it.ID, done, total)
			if d, t := it.DeliverableCounts(); t > 0 {
				fmt.Printf(" · deliverables %d/%d", d, t)
			}
			fmt.Println()
			for i, c := range it.Checks {
				mark := "☐"
				switch {
				case c.Done:
					mark = "☑"
				case c.Waived != "":
					mark = "⊘"
				}
				hand := ""
				if c.Manual {
					hand = "✋ "
				}
				line := fmt.Sprintf("  %d %s %s%s", i+1, mark, hand, c.Text)
				if c.Evidence != "" {
					line += "  (" + c.Evidence + ")"
				}
				if c.Waived != "" {
					line += "  (waived: " + c.Waived + ")"
				}
				if c.For > 0 {
					line += fmt.Sprintf("  → deliverable %d", c.For)
				}
				fmt.Println(line)
				if c.How != "" {
					fmt.Println("      how: " + c.How)
				}
			}
			return nil
		},
	}
}

func (a *app) measureCmd() *cobra.Command {
	var id, target, evidence string
	var remove bool
	cmd := &cobra.Command{
		Use:   "measure <name> [current]",
		Short: "Record a measurable: what was targeted and what was reached",
		Long: `Add or update a row in the ticket's Measurables table.

  shp measure "Endpoints migrated" 3/5 --target 5/5
  shp measure "Endpoints migrated" 5/5 --evidence a1b2c3d
  shp measure "Test coverage" 84% --target 80% --evidence ci/run/123
  shp measure "Endpoints migrated" --rm`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.load(); err != nil {
				return err
			}
			it, err := a.resolve([]string{id})
			if err != nil {
				return err
			}
			name := strings.TrimSpace(args[0])
			idx := -1
			for i, m := range it.Measures {
				if strings.EqualFold(m.Name, name) {
					idx = i
				}
			}
			if remove {
				if idx < 0 {
					return fmt.Errorf("%s has no measure %q", it.ID, name)
				}
				it.Measures = append(it.Measures[:idx], it.Measures[idx+1:]...)
				if err := a.store.Save(it); err != nil {
					return err
				}
				return a.done(it)
			}
			if idx < 0 {
				it.Measures = append(it.Measures, model.Measure{Name: name})
				idx = len(it.Measures) - 1
			}
			m := &it.Measures[idx]
			if len(args) > 1 {
				m.Current = args[1]
			}
			if target != "" {
				m.Target = target
			}
			if evidence != "" {
				m.Evidence = evidence
			}
			msg := m.Name + ": " + m.Current
			if m.Target != "" {
				msg += " (target " + m.Target + ")"
			}
			if met, known := m.Met(); known && met {
				msg += " ✓"
			}
			var detail []string
			if m.Evidence != "" {
				detail = []string{"evidence: " + m.Evidence}
			}
			return a.saveAndRecord(it, journal.Event{Type: "measure", Message: msg, Detail: detail})
		},
	}
	a.itemFlag(cmd, &id)
	cmd.Flags().StringVar(&target, "target", "", "the goal, e.g. 2, 2/2, ≥95%, yes")
	cmd.Flags().StringVarP(&evidence, "evidence", "e", "", "commit, PR, path or URL")
	cmd.Flags().BoolVar(&remove, "rm", false, "remove the measure")
	return cmd
}

func (a *app) visualCmd() *cobra.Command {
	var fromStdin, auto bool
	cmd := &cobra.Command{
		Use:   "visual [id]",
		Short: "Set an item's Visual section (diagram) from stdin, or print the generated one",
		Long: `Every story and epic shows a Visual. Without a hand-written one, Sherpa
generates it live from the children, their status and dependencies.

  shp visual S-004 --stdin < diagram.txt   set a custom diagram (kept as preformatted text)
  shp visual S-004 --auto                  print the generated diagram`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.load(); err != nil {
				return err
			}
			it, err := a.resolve(args)
			if err != nil {
				return err
			}
			if auto || !fromStdin {
				all, err := a.store.All()
				if err != nil {
					return err
				}
				fmt.Println(render.AutoVisual(it, all))
				return nil
			}
			data, err := io.ReadAll(os.Stdin)
			if err != nil {
				return err
			}
			text := strings.Trim(string(data), "\n")
			if strings.TrimSpace(text) == "" {
				return errors.New("empty diagram on stdin")
			}
			if !strings.HasPrefix(strings.TrimSpace(text), "```") {
				text = "```text\n" + text + "\n```"
			}
			it.Body = model.SetSection(it.Body, "Visual", text)
			return a.saveAndRecord(it, journal.Event{Type: "edit", Message: "updated the Visual"})
		},
	}
	cmd.Flags().BoolVar(&fromStdin, "stdin", false, "read the diagram from stdin")
	cmd.Flags().BoolVar(&auto, "auto", false, "print the generated diagram")
	return cmd
}

func (a *app) dependsCmd() *cobra.Command {
	var remove bool
	cmd := &cobra.Command{
		Use:   "depends <id> <dependency...>",
		Short: "Record that an item depends on others (shown in Visuals and ready checks)",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.load(); err != nil {
				return err
			}
			it, err := a.store.Get(args[0])
			if err != nil {
				return err
			}
			for _, d := range args[1:] {
				dep, err := a.store.Get(d)
				if err != nil {
					return err
				}
				i := indexOf(it.Depends, dep.ID)
				switch {
				case remove && i >= 0:
					it.Depends = append(it.Depends[:i], it.Depends[i+1:]...)
				case !remove && i < 0 && dep.ID != it.ID:
					it.Depends = append(it.Depends, dep.ID)
				}
			}
			if err := a.store.Save(it); err != nil {
				return err
			}
			return a.done(it)
		},
	}
	cmd.Flags().BoolVar(&remove, "rm", false, "remove the dependencies instead")
	return cmd
}

func indexOf(xs []string, x string) int {
	for i, v := range xs {
		if v == x {
			return i
		}
	}
	return -1
}
