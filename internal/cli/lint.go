package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/RL-Charles/sherpa/internal/model"
)

// Lint checks the ticket brief itself, not the work. It warns by default and
// only fails with --strict, so it can be adopted without blocking old work.

type finding struct {
	ID      string `json:"id"`
	Level   string `json:"level"` // warning or error
	Message string `json:"message"`
}

func (a *app) lintCmd() *cobra.Command {
	var strict, all bool
	cmd := &cobra.Command{
		Use:   "lint [id]",
		Short: "Check a ticket brief for missing context, deliverables, checks and code map",
		Long: `Lint the brief, not the code. It looks for the things a reader needs:
why the work exists, the context and design, the deliverables, evidence for
checks, and where the change lands.

With no id it lints open tickets (backlog, ready, in progress, blocked and review).
Use --all to include done tickets.

  shp lint T-0030
  shp lint --strict        exit non-zero if anything is missing
  shp lint --all           include done tickets`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.load(); err != nil {
				return err
			}
			var items []*model.Item
			if len(args) == 1 {
				it, err := a.store.Get(args[0])
				if err != nil {
					return err
				}
				items = []*model.Item{it}
			} else {
				allItems, err := a.store.All()
				if err != nil {
					return err
				}
				for _, it := range allItems {
					if it.Kind != model.Ticket {
						continue
					}
					if all || openForLint(it.Status) {
						items = append(items, it)
					}
				}
			}

			var findings []finding
			for _, it := range items {
				findings = append(findings, lintItem(it)...)
			}
			if a.json {
				if err := a.emit(findings); err != nil {
					return err
				}
			} else {
				printFindings(items, findings)
			}
			if strict && len(findings) > 0 {
				return fmt.Errorf("lint: %d finding%s", len(findings), plural(len(findings)))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&strict, "strict", false, "exit non-zero when there are findings")
	cmd.Flags().BoolVar(&all, "all", false, "include done tickets")
	return cmd
}

func openForLint(s model.Status) bool { return s != model.Done }

func lintItem(it *model.Item) []finding {
	var out []finding
	add := func(level, format string, args ...any) {
		out = append(out, finding{ID: it.ID, Level: level, Message: fmt.Sprintf(format, args...)})
	}
	type required struct{ name, why string }
	var reqs []required
	spike := it.Template == "spike"
	switch it.Template {
	case "bug":
		reqs = []required{
			{"Expected", "say what should happen"},
			{"Actual", "say what happens instead, with the error"},
			{"Repro", "give the steps that reproduce it"},
			{"Design", "name the suspected cause and the regression test"},
			{"Out of scope", "say what this fix deliberately is not"},
		}
	case "spike":
		reqs = []required{
			{"Question", "name the decision this spike unblocks"},
			{"Timebox", "say how long and when it ends"},
			{"Context", "list background, constraints and prior art"},
			{"Findings", "record what was learned"},
			{"Recommendation", "say what to do next"},
			{"Out of scope", "say what this spike deliberately is not"},
		}
	case "chore":
		reqs = []required{
			{"Why", "say what maintenance or operational need this serves"},
			{"Change", "say what changes and what deliberately does not"},
			{"Design notes", "name the types, invariants and edge cases"},
			{"Verification", "say how we will know it worked"},
			{"Blast radius", "say what could break and how to roll back"},
			{"Out of scope", "say what this deliberately is not"},
		}
	default:
		reqs = []required{
			{"Why", "say what hurts, for whom, and what changes"},
			{"Context", "list background, constraints, prior art and related tickets"},
			{"Design", "show the shape, types/enums, invariants, contracts and edge cases"},
			{"Out of scope", "say what this deliberately is not"},
		}
	}
	for _, s := range reqs {
		if _, ok := model.Section(it.Body, s.name); !ok {
			add("warning", "no %s section: %s", s.name, s.why)
		}
	}
	if !spike && len(it.Deliverables) == 0 {
		add("warning", "no deliverables: state the outcomes with `shp deliverable add …`")
	}
	for i := range it.Deliverables {
		if len(it.ChecksFor(i+1)) == 0 {
			add("warning", "deliverable %d has no check: `shp check add --for %d \"…\"`", i+1, i+1)
		}
		r, t := it.CheckCountsFor(i + 1)
		if it.Deliverables[i].Done && r < t {
			add("warning", "deliverable %d is marked done but %d/%d checks are open", i+1, r, t)
		}
		if !it.Deliverables[i].Done && t > 0 && r == t {
			add("warning", "all checks for deliverable %d are resolved: `shp deliverable done %d`", i+1, i+1)
		}
	}
	for i, c := range it.Checks {
		if c.For < 0 || c.For > len(it.Deliverables) {
			add("error", "check %d points at deliverable %d, but there are %d", i+1, c.For, len(it.Deliverables))
		}
		if c.Done && !c.Manual && c.Waived == "" && strings.TrimSpace(c.Evidence) == "" {
			add("warning", "check %d is ticked without evidence: `shp check %d -e <ref>`", i+1, i+1)
		}
		if vagueCheck(c.Text) && strings.TrimSpace(c.Evidence) == "" {
			add("warning", "check %d is vague (%q): name the command, test or observable result", i+1, c.Text)
		}
	}
	if !spike && len(it.Code) == 0 && (it.Status == model.InProgress || it.Status == model.Review || it.Status == model.AgentReview) {
		add("warning", "no code map: `shp where add <path> --symbols … --deliverable N`")
	}
	if it.Now == "" && (it.Status == model.InProgress || it.Status == model.Review || it.Status == model.AgentReview) {
		add("warning", "no Now line: `shp now \"…\"`")
	}
	if p := templatePlaceholder(it.Body); p != "" {
		add("warning", "brief still contains a template placeholder: %q", p)
	}
	if _, ok := model.Section(it.Body, "Deliverables"); ok && len(it.Deliverables) > 0 {
		add("warning", "body has a manual Deliverables section; the structured list is generated (move the prose to Why/Context)")
	}
	if _, ok := model.Section(it.Body, "Where"); ok && len(it.Code) > 0 {
		add("warning", "body has a manual Where section; the code map is generated (keep notes in the code entries)")
	}
	if _, ok := model.Section(it.Body, "Checks"); ok && len(it.Checks) > 0 {
		add("warning", "body has a manual Checks section; checks are generated (move criteria to `shp check`)")
	}
	return out
}

// templatePlaceholder finds the first unfilled prompt from a ticket template,
// so a ticket does not look complete just because headings exist.
func templatePlaceholder(body string) string {
	for _, p := range []string{
		"_What hurts today",
		"_One paragraph",
		"_Show the new or changed types",
		"_Name the pattern",
		"_Only if the fix changes",
		"_What must always be true?",
		"_Empty, expired",
		"_What did you consider",
		"_What should happen?",
		"_What happens instead?",
		"_Which code path",
		"_How long, and what is the deadline?",
		"_What decision should this spike unblock?",
		"_What did we learn?",
		"_What should we do next",
		"_What maintenance or operational need",
		"_What changes, and what deliberately",
		"_How will we know it worked?",
		"_What could break",
	} {
		if strings.Contains(body, p) {
			return p
		}
	}
	// A bullet label with nothing after the colon is still a prompt:
	// "Constraints:" is not a constraint. A following indented line counts
	// as the value.
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		t := strings.TrimSpace(line)
		if t == "- …" || t == "- ..." {
			return t
		}
		if strings.HasPrefix(t, "- ") && strings.HasSuffix(t, ":") && len(t) > 3 {
			if i+1 < len(lines) && strings.HasPrefix(lines[i+1], " ") && strings.TrimSpace(lines[i+1]) != "" {
				continue
			}
			return t
		}
	}
	return ""
}

// vagueCheck catches checks that assert a feeling rather than an observable.
func vagueCheck(text string) bool {
	t := strings.ToLower(strings.TrimSpace(text))
	switch t {
	case "works", "work", "it works", "tests pass", "all tests pass", "looks good", "done", "fixed", "passes":
		return true
	}
	return strings.HasPrefix(t, "works ") || strings.HasSuffix(t, " works")
}

func printFindings(items []*model.Item, findings []finding) {
	if len(items) == 0 {
		fmt.Println("No tickets to lint.")
		return
	}
	if len(findings) == 0 {
		fmt.Printf("✓ %d ticket%s clean\n", len(items), plural(len(items)))
		return
	}
	byItem := map[string][]finding{}
	for _, f := range findings {
		byItem[f.ID] = append(byItem[f.ID], f)
	}
	for _, it := range items {
		fs := byItem[it.ID]
		if len(fs) == 0 {
			continue
		}
		fmt.Printf("%s · %s\n", it.ID, it.Title)
		for _, f := range fs {
			icon := "⚠"
			if f.Level == "error" {
				icon = "✖"
			}
			fmt.Printf("  %s %s\n", icon, f.Message)
		}
		fmt.Println()
	}
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
