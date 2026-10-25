package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/RL-Charles/sherpa/internal/journal"
	"github.com/RL-Charles/sherpa/internal/model"
)

// Design is the small, durable part of the brief that shows understanding:
// the shape, types/enums, invariants, contracts, edge cases and alternatives.
// Keeping it a section-scoped command means an agent can update it without
// rewriting the rest of the ticket.

func (a *app) designCmd() *cobra.Command {
	var fromStdin bool
	cmd := &cobra.Command{
		Use:   "design [id]",
		Short: "Print or set a ticket's Design section (types, enums, invariants, edge cases)",
		Long: `Read or replace the ## Design section without touching the rest of the body.

  shp design T-0030                  print it
  shp design T-0030 --stdin < design.md

Put the shape first, then the types/enums the change introduces, the invariants
that must hold, the contracts at the boundary, edge cases and alternatives.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.load(); err != nil {
				return err
			}
			it, err := a.resolve(args)
			if err != nil {
				return err
			}
			if !fromStdin {
				text, ok := model.Section(it.Body, "Design")
				if !ok || strings.TrimSpace(text) == "" {
					fmt.Printf("%s has no Design section yet. Write one with `shp design %s --stdin`.\n", it.ID, it.ID)
					return nil
				}
				fmt.Println(strings.TrimSpace(text))
				return nil
			}
			data, err := io.ReadAll(os.Stdin)
			if err != nil {
				return err
			}
			text := strings.TrimSpace(string(data))
			if text == "" {
				return errors.New("empty Design section on stdin")
			}
			// Accept a pasted "## Design" heading as well as the body alone.
			if lines := strings.SplitN(text, "\n", 2); strings.EqualFold(strings.TrimSpace(lines[0]), "## Design") {
				if len(lines) == 1 {
					return errors.New("empty Design section on stdin")
				}
				text = strings.TrimSpace(lines[1])
			}
			it.Body = model.SetSection(it.Body, "Design", text)
			return a.saveAndRecord(it, journal.Event{Type: "design", Message: "updated the Design"})
		},
	}
	cmd.Flags().BoolVar(&fromStdin, "stdin", false, "read the Design section from stdin")
	return cmd
}
