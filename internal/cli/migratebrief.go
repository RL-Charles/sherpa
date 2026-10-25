package cli

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/RL-Charles/sherpa/internal/journal"
	"github.com/RL-Charles/sherpa/internal/model"
)

// Migrate brief -----------------------------------------------------------------
//
// Existing tickets predate deliverables and the code map. This command
// recovers what the repository already knows (checks, touched files, linked
// PRs, section shape) and never invents prose. Preview is the default; --write
// applies the mechanical fields and journals it as a migration.

type briefChange struct {
	ID            string   `json:"id"`
	Title         string   `json:"title"`
	Deliverable   string   `json:"deliverable,omitempty"`
	LinkedChecks  int      `json:"linked_checks,omitempty"`
	Files         []string `json:"files,omitempty"`
	Template      string   `json:"template,omitempty"`
	Description   bool     `json:"normalized_description,omitempty"`
	Now           string   `json:"now,omitempty"`
	RemainingGaps []string `json:"remaining_gaps,omitempty"`
}

type briefMigrateOptions struct {
	FromHistory          bool
	DeliverableFromTitle bool
	LinkChecks           bool
	StampTemplate        bool
	NormalizeDescription bool
}

func (a *app) migrateBriefCmd() *cobra.Command {
	var opts briefMigrateOptions
	var write, all bool
	cmd := &cobra.Command{
		Use:   "brief [id...]",
		Short: "Preview or apply the mechanical part of the new ticket brief to existing tickets",
		Long: `Add deliverables and a code map to existing tickets without inventing prose.

The migration can recover facts the repository already has:
  - a deliverable from the ticket title (optional)
  - checks linked to that deliverable (optional)
  - code-map entries from files recorded by hooks and worklog entries
  - the ticket template from its existing section headings
  - a Now line from the latest log entry
  - Description → Why when Why is missing (optional)

It never writes Why, Context, Design or Out of scope. Those need a human or
an agent who understands the work; the report lists them as remaining gaps.

Preview is the default. --write applies the mechanical fields. Writing every
ticket also requires --all.

  shp migrate brief T-0046
  shp migrate brief T-0046 --deliverable-from-title --write
  shp migrate brief --all --write`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.load(); err != nil {
				return err
			}
			if write && len(args) == 0 && !all {
				return errors.New("refusing to write every ticket without --all; pass ticket IDs or --all")
			}
			var items []*model.Item
			if len(args) > 0 {
				for _, id := range args {
					it, err := a.store.Get(id)
					if err != nil {
						return err
					}
					if it.Kind != model.Ticket {
						return fmt.Errorf("%s is a %s; migrate brief applies to tickets", it.ID, it.Kind)
					}
					items = append(items, it)
				}
			} else {
				allItems, err := a.store.All()
				if err != nil {
					return err
				}
				for _, it := range allItems {
					if it.Kind == model.Ticket {
						items = append(items, it)
					}
				}
			}
			events, err := journal.ReadAll(a.store.Dir)
			if err != nil {
				return err
			}

			var changes []briefChange
			for _, it := range items {
				ch := migrateOneBrief(it, events, opts)
				if ch.Deliverable == "" && len(ch.Files) == 0 && ch.Template == "" && !ch.Description && ch.Now == "" {
					continue
				}
				changes = append(changes, ch)
				if write {
					if err := a.store.Write(it); err != nil { // format upgrade, not an edit
						return err
					}
					msg := briefChangeMessage(ch)
					if err := a.record(journal.Event{Item: it.ID, Type: "migrate", Message: msg}); err != nil {
						return err
					}
				}
			}

			if a.json {
				return a.emit(changes)
			}
			printBriefChanges(changes, write)
			return nil
		},
	}
	cmd.Flags().BoolVar(&write, "write", false, "apply the mechanical changes (default: preview)")
	cmd.Flags().BoolVar(&all, "all", false, "include every ticket; required with --write when no IDs are given")
	cmd.Flags().BoolVar(&opts.FromHistory, "from-history", true, "add code-map entries from files recorded in the journal")
	cmd.Flags().BoolVar(&opts.DeliverableFromTitle, "deliverable-from-title", false, "create one deliverable from the ticket title")
	cmd.Flags().BoolVar(&opts.LinkChecks, "link-checks", true, "link unlinked checks to the new deliverable")
	cmd.Flags().BoolVar(&opts.StampTemplate, "stamp-template", true, "detect and record the ticket template from its sections")
	cmd.Flags().BoolVar(&opts.NormalizeDescription, "normalize-description", false, "move a real Description section to Why when Why is missing")
	return cmd
}

func migrateOneBrief(it *model.Item, events []journal.Event, opts briefMigrateOptions) briefChange {
	ch := briefChange{ID: it.ID, Title: it.Title}

	if opts.NormalizeDescription {
		ch.Description = normalizeDescription(it)
	}
	if opts.StampTemplate && it.Template == "" {
		if t := detectTemplate(it); t != "" {
			it.Template = t
			ch.Template = t
		}
	}
	if opts.DeliverableFromTitle && len(it.Deliverables) == 0 && strings.TrimSpace(it.Title) != "" {
		d := model.Deliverable{Text: strings.TrimSpace(it.Title)}
		if it.Status == model.Done {
			d.Done = true
			d.Evidence = "done before brief"
			if it.Links != nil && it.Links.PRNumber > 0 {
				d.Evidence = fmt.Sprintf("PR #%d", it.Links.PRNumber)
			}
		}
		it.Deliverables = append(it.Deliverables, d)
		ch.Deliverable = d.Text
		if opts.LinkChecks {
			for i := range it.Checks {
				if it.Checks[i].For == 0 {
					it.Checks[i].For = 1
					ch.LinkedChecks++
				}
			}
		}
	}
	if opts.FromHistory {
		files := journalFiles(events, it.ID)
		existing := map[string]bool{}
		for _, c := range it.Code {
			existing[filepath.ToSlash(filepath.Clean(c.Path))] = true
		}
		state := "touched"
		if it.Status == model.Done {
			state = "done"
		}
		for _, f := range files {
			f = filepath.ToSlash(filepath.Clean(f))
			if f == "" || strings.HasPrefix(f, ".sherpa/") || existing[f] {
				continue
			}
			ref := model.CodeRef{Path: f, Change: "modify", Status: state}
			if len(it.Deliverables) == 1 {
				ref.Deliverable = 1
			}
			it.Code = append(it.Code, ref)
			existing[f] = true
			ch.Files = append(ch.Files, f)
		}
	}
	if it.Now == "" && it.Status != model.Done && it.Status != model.Backlog {
		for _, e := range journal.ForItem(events, it.ID) {
			if e.Type == "log" && e.Message != "" {
				it.Now = e.Message
			}
		}
		if it.Now != "" {
			ch.Now = it.Now
		}
	}
	for _, f := range lintItem(it) {
		ch.RemainingGaps = append(ch.RemainingGaps, f.Message)
	}
	return ch
}

// normalizeDescription moves an authored Description to Why when Why is
// missing. It never touches placeholder descriptions.
func normalizeDescription(it *model.Item) bool {
	if _, ok := model.Section(it.Body, "Why"); ok {
		return false
	}
	desc, ok := model.Section(it.Body, "Description")
	if !ok {
		return false
	}
	desc = strings.TrimSpace(desc)
	if desc == "" || strings.Contains(strings.ToLower(desc), "no description yet") || strings.HasPrefix(desc, "_") {
		return false
	}
	it.Body = model.SetSection(it.Body, "Why", desc)
	it.Body = model.RemoveSection(it.Body, "Description")
	return true
}

func detectTemplate(it *model.Item) string {
	has := func(name string) bool {
		_, ok := model.Section(it.Body, name)
		return ok
	}
	switch {
	case has("Expected") && has("Actual") && has("Repro"):
		return "bug"
	case has("Question") && has("Findings") && has("Recommendation"):
		return "spike"
	case has("Change") && has("Blast radius"):
		return "chore"
	case has("Why") || has("Design") || has("Context"):
		return "feature"
	}
	return ""
}

func journalFiles(events []journal.Event, id string) []string {
	seen := map[string]bool{}
	var out []string
	for _, e := range journal.ForItem(events, id) {
		for _, f := range e.Files {
			f = filepath.ToSlash(filepath.Clean(strings.TrimSpace(f)))
			if f == "" || seen[f] {
				continue
			}
			seen[f] = true
			out = append(out, f)
		}
	}
	return out
}

func briefChangeMessage(ch briefChange) string {
	var parts []string
	if ch.Deliverable != "" {
		p := "deliverable from title"
		if ch.LinkedChecks > 0 {
			p += fmt.Sprintf(" (+%d checks)", ch.LinkedChecks)
		}
		parts = append(parts, p)
	}
	if len(ch.Files) > 0 {
		parts = append(parts, fmt.Sprintf("%d code-map files from history", len(ch.Files)))
	}
	if ch.Template != "" {
		parts = append(parts, "template "+ch.Template)
	}
	if ch.Description {
		parts = append(parts, "Description → Why")
	}
	if ch.Now != "" {
		parts = append(parts, "Now from latest log")
	}
	return "brief backfill: " + strings.Join(parts, ", ")
}

func printBriefChanges(changes []briefChange, wrote bool) {
	if len(changes) == 0 {
		fmt.Println("No mechanical brief changes. Remaining prose gaps are listed by `shp lint`.")
		return
	}
	for _, ch := range changes {
		fmt.Printf("%s · %s\n", ch.ID, ch.Title)
		if ch.Deliverable != "" {
			fmt.Printf("  + deliverable 1 (from title): %s\n", ch.Deliverable)
			if ch.LinkedChecks > 0 {
				fmt.Printf("    linked %d check%s\n", ch.LinkedChecks, plural(ch.LinkedChecks))
			}
		}
		if len(ch.Files) > 0 {
			fmt.Printf("  + where: %d file%s from history\n", len(ch.Files), plural(len(ch.Files)))
			for i, f := range ch.Files {
				if i == 4 {
					fmt.Printf("    … %d more\n", len(ch.Files)-4)
					break
				}
				fmt.Printf("    %s\n", f)
			}
		}
		if ch.Template != "" {
			fmt.Printf("  ~ template: %s\n", ch.Template)
		}
		if ch.Description {
			fmt.Println("  ~ Description → Why")
		}
		if ch.Now != "" {
			fmt.Println("  ~ Now from latest log")
		}
		if len(ch.RemainingGaps) > 0 {
			for i, g := range ch.RemainingGaps {
				if i == 2 {
					fmt.Printf("  ! … %d more gap%s (shp lint %s)\n", len(ch.RemainingGaps)-2, plural(len(ch.RemainingGaps)-2), ch.ID)
					break
				}
				fmt.Printf("  ! %s\n", g)
			}
		}
		fmt.Println()
	}
	verb := "would change"
	if wrote {
		verb = "changed"
	}
	fmt.Printf("%d ticket%s %s. ", len(changes), plural(len(changes)), verb)
	if !wrote {
		fmt.Println("Re-run with the same IDs and --write to apply the mechanical fields.")
	} else {
		fmt.Println("Prose gaps still need a human or an agent: `shp lint --strict`.")
	}
}
