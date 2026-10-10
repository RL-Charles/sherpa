package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/RL-Charles/sherpa/internal/journal"
	"github.com/RL-Charles/sherpa/internal/links"
	"github.com/RL-Charles/sherpa/internal/model"
)

// backfillCmd brings existing items up to the structured format where the
// data allows it. It is idempotent and never invents results:
//   - Acceptance sections become checks (ticked for done items, citing the PR)
//   - Dependencies sections become depends links
//   - PR URLs in archived Agency comments become links (primary + related)
//   - in-progress items without a Now line get their latest log headline
func (a *app) backfillCmd() *cobra.Command {
	var dry bool
	cmd := &cobra.Command{
		Use:    "backfill",
		Short:  "Convert existing items to checks, dependencies and PR links",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.load(); err != nil {
				return err
			}
			items, err := a.store.All()
			if err != nil {
				return err
			}
			events, err := journal.ReadAll(a.store.Dir)
			if err != nil {
				return err
			}
			byLabel := map[string]string{}
			for _, it := range items {
				for _, l := range it.Labels {
					byLabel[strings.ToUpper(l)] = it.ID
				}
			}
			counts := map[string]int{}
			for _, it := range items {
				before := fmt.Sprintf("%+v %+v %v %s %q", it.Checks, it.Links, it.Depends, it.Now, it.Body)
				var notes []string

				// Dependencies section → depends (keys or ticket IDs).
				if dep, ok := model.Section(it.Body, "Dependencies"); ok && len(it.Depends) == 0 {
					for _, k := range regexp.MustCompile(`\b([A-Z]+-\d+)\b`).FindAllString(dep, -1) {
						id := byLabel[k]
						if id == "" {
							if norm, _, _, err := model.ParseID(k); err == nil {
								id = norm
							}
						}
						if _, err := a.store.Get(id); err == nil && id != it.ID && !slices.Contains(it.Depends, id) {
							it.Depends = append(it.Depends, id)
						}
					}
					if len(it.Depends) > 0 {
						notes = append(notes, "depends")
						counts["depends"]++
					}
					// The section is now the structured depends list; drop it
					// unless it says more than the keys ("None." is dropped too).
					rest := regexp.MustCompile(`\b[A-Z]+-\d+\b|[,.;\s]|and|None`).ReplaceAllString(dep, "")
					if rest == "" {
						it.Body = model.RemoveSection(it.Body, "Dependencies")
					}
				}

				// PR URLs in the archived Agency issue → links.
				for _, l := range it.Labels {
					data, err := os.ReadFile(filepath.Join(a.store.Dir, "imports", "agency", strings.ToUpper(l)+".md"))
					if err != nil {
						continue
					}
					urls := links.AllFromText(string(data))
					if len(urls) == 0 {
						continue
					}
					if it.Links == nil {
						it.Links = &model.Links{}
					}
					if it.Links.PR == "" {
						primary := urls[len(urls)-1] // the latest mention is the delivering PR
						it.Links.PR = primary
						_, num := links.FromText(primary)
						it.Links.PRNumber = num
						notes = append(notes, "PR")
						counts["prs"]++
					}
					for _, u := range urls {
						if u != it.Links.PR && !slices.Contains(it.Links.Related, u) {
							it.Links.Related = append(it.Links.Related, u)
						}
					}
				}

				// Acceptance → checks.
				if len(it.Checks) == 0 {
					if checks, rest, ok := model.ExtractChecks(it.Body); ok {
						if it.Status == model.Done {
							ev := "done before Sherpa"
							if it.Links != nil && it.Links.PRNumber > 0 {
								ev = fmt.Sprintf("PR #%d", it.Links.PRNumber)
							}
							for i := range checks {
								if !checks[i].Done {
									checks[i].Done, checks[i].Evidence = true, ev
								}
							}
						}
						it.Checks, it.Body = checks, rest
						if len(checks) > 0 {
							notes = append(notes, fmt.Sprintf("%d checks", len(checks)))
							counts["checks"] += len(checks)
						} else {
							counts["placeholders removed"]++
						}
					}
				}

				// Now line for work in flight: its latest log headline.
				if it.Status == model.InProgress && it.Now == "" {
					for _, e := range journal.ForItem(events, it.ID) {
						if e.Type == "log" && e.Message != "" {
							it.Now = e.Message
						}
					}
					if it.Now != "" {
						notes = append(notes, "now")
						counts["now"]++
					}
				}

				if fmt.Sprintf("%+v %+v %v %s %q", it.Checks, it.Links, it.Depends, it.Now, it.Body) == before {
					continue
				}
				counts["items"]++
				if !a.json {
					fmt.Printf("%s %-8s %s\n", map[bool]string{true: "~", false: "✚"}[dry], it.ID, strings.Join(notes, ", "))
				}
				if !dry {
					if err := a.store.Write(it); err != nil { // keep Updated: a format upgrade, not an edit
						return err
					}
					if err := a.record(journal.Event{Item: it.ID, Type: "migrate", Message: "backfilled: " + strings.Join(notes, ", ")}); err != nil {
						return err
					}
				}
			}
			if a.json {
				return a.emit(counts)
			}
			fmt.Printf("%d items · %d checks · %d with depends · %d PRs from history · %d Now lines · %d empty placeholders removed\n",
				counts["items"], counts["checks"], counts["depends"], counts["prs"], counts["now"], counts["placeholders removed"])
			return nil
		},
	}
	cmd.Flags().BoolVar(&dry, "dry-run", false, "show what would change")
	return cmd
}
