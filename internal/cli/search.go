package cli

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/RL-Charles/sherpa/internal/render"
	"github.com/RL-Charles/sherpa/internal/search"
)

var hitStyle = lipgloss.NewStyle().Bold(true).Underline(true)

// Highlight emphasises the runes of s at positions hits.
func Highlight(s string, hits []int) string {
	if len(hits) == 0 {
		return s
	}
	at := map[int]bool{}
	for _, h := range hits {
		at[h] = true
	}
	var b strings.Builder
	for i, r := range []rune(s) {
		if at[i] {
			b.WriteString(hitStyle.Render(string(r)))
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func (a *app) searchCmd() *cobra.Command {
	var limit int
	cmd := &cobra.Command{
		Use:     "search <query...>",
		Aliases: []string{"find"},
		Short:   "Fuzzy-search items by ID, title, labels and assignee, then body text",
		Long: `Fuzzy-search items. Letters must appear in order; gaps are fine.

  shp search oauth login      titles, labels, assignees (best match first)
  shp search 30               the item T-0030 (also t30, T-0030)
  shp search is:ready auth    only ready items; is:<status> or is:<epic|story|ticket>
  shp search @claude          only items assigned to claude
  shp search pkce             also finds words inside ticket text

In the TUI, press / to search as you type.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.load(); err != nil {
				return err
			}
			items, err := a.store.All()
			if err != nil {
				return err
			}
			results := search.Search(items, strings.Join(args, " "), limit)
			if a.json {
				type hit struct {
					ID, Kind, Status, Title, Snippet string
					Score                            int
				}
				out := []hit{}
				for _, r := range results {
					out = append(out, hit{r.Item.ID, string(r.Item.Kind), string(r.Item.Status), r.Item.Title, r.Snippet, r.Score})
				}
				return a.emit(out)
			}
			if len(results) == 0 {
				fmt.Println("No matches.")
				return nil
			}
			for _, r := range results {
				it := r.Item
				line := fmt.Sprintf("%s %s %s %s", render.StatusStyle(it.Status).Render(it.Status.Glyph()), it.ID,
					Highlight(it.Title, r.Hits), render.StatusStyle(it.Status).Render(string(it.Status)))
				fmt.Println(line)
				if r.Snippet != "" {
					fmt.Println("    " + lipgloss.NewStyle().Faint(true).Render("in text: "+r.Snippet))
				}
			}
			return nil
		},
	}
	cmd.Flags().IntVarP(&limit, "limit", "n", 20, "maximum results (0 = all)")
	return cmd
}
