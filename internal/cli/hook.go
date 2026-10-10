package cli

import (
	"fmt"
	"io"
	"os"
	"slices"

	"github.com/spf13/cobra"

	"github.com/RL-Charles/sherpa/internal/hooks"
	"github.com/RL-Charles/sherpa/internal/workflow"
)

// hookCmd is what agent hook systems and git call. It must never break the
// caller: unknown agents, bad payloads and missing projects all fail open.
func (a *app) hookCmd() *cobra.Command {
	var agent string
	cmd := &cobra.Command{
		Use:    "hook <session-start|pre-edit|post-edit|stop|post-shell|working|commit-msg> [args]",
		Short:  "Entry point for agent and git hooks (installed by `shp hooks install`)",
		Hidden: true,
		Args:   cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// A Go panic exits 2, which Claude Code and Codex read as "block".
			// A crash in Sherpa must never stop an agent.
			defer func() {
				if r := recover(); r != nil {
					fmt.Fprintln(os.Stderr, "sherpa hook error:", r)
					os.Exit(0)
				}
			}()
			if args[0] == "commit-msg" {
				if len(args) < 2 {
					return nil
				}
				dir, _ := os.Getwd()
				if err := hooks.CommitMsg(dir, args[1], workflow.CurrentActor().Kind == workflow.Agent); err != nil {
					fmt.Fprintln(os.Stderr, err)
					os.Exit(1)
				}
				return nil
			}
			ev := hooks.Event(args[0])
			if !slices.Contains(hooks.Events, ev) {
				return nil
			}
			ad, ok := hooks.Adapters[agent]
			if !ok {
				return nil
			}
			// The OpenCode plugin passes its payload in the environment; other
			// agents write it to stdin.
			payload := []byte(os.Getenv("SHERPA_HOOK_PAYLOAD"))
			if len(payload) == 0 {
				payload, _ = io.ReadAll(io.LimitReader(os.Stdin, 4<<20))
			}
			in := ad.Parse(ev, payload)
			in.Agent, in.Event = agent, ev
			os.Exit(ad.Write(ev, hooks.Handle(in), os.Stdout, os.Stderr))
			return nil
		},
	}
	cmd.Flags().StringVar(&agent, "agent", "claude", "calling agent: claude, cursor, codex, opencode")
	return cmd
}

func (a *app) hooksCmd() *cobra.Command {
	var agents []string
	var noGit, noDocs bool
	cmd := &cobra.Command{
		Use:   "hooks",
		Short: "Install Sherpa into Claude Code, Cursor, Codex and OpenCode",
		Long: `Wire Sherpa into coding agents so tracking is enforced, not just requested:

  session start  → the agent is told what's active and ready
  before an edit → blocked until a ticket is claimed (shp start)
  after an edit  → the file is recorded on the ticket
  on stop        → asked once to log or submit unlogged changes
  git commit     → a Ticket: trailer is added (agents can't commit without one)

Files written (in the checkout holding .sherpa):
  claude    .claude/settings.json
  cursor    .cursor/hooks.json
  codex     .codex/hooks.json   (approve in Codex's /hooks; project must be trusted)
  opencode  .opencode/plugins/sherpa.ts
  git       .git/hooks/commit-msg
  docs      AGENTS.md protocol section, CLAUDE.md → @AGENTS.md`,
	}
	print := func(results []hooks.Result) error {
		if a.json {
			return a.emit(results)
		}
		glyph := map[string]string{"installed": "✚", "updated": "↻", "unchanged": "✓", "removed": "✖", "absent": "·", "skipped": "⚠"}
		for _, r := range results {
			line := fmt.Sprintf("%s %-9s %-10s %s", glyph[r.State], r.Target, r.State, r.Path)
			if r.Note != "" {
				line += "  — " + r.Note
			}
			fmt.Println(line)
		}
		return nil
	}
	install := &cobra.Command{
		Use:   "install",
		Short: "Install or update hooks (idempotent; keeps your other settings)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.load(); err != nil {
				return err
			}
			return print(hooks.Install(a.store.Root, agents, !noGit, !noDocs))
		},
	}
	uninstall := &cobra.Command{
		Use:   "uninstall",
		Short: "Remove Sherpa's hooks (other hooks and the docs are left alone)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.load(); err != nil {
				return err
			}
			return print(hooks.Uninstall(a.store.Root, agents, !noGit))
		},
	}
	status := &cobra.Command{
		Use:   "status",
		Short: "Show which agent integrations are installed",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.load(); err != nil {
				return err
			}
			return print(hooks.Status(a.store.Root))
		},
	}
	for _, c := range []*cobra.Command{install, uninstall} {
		c.Flags().StringSliceVar(&agents, "agent", hooks.Agents, "agents to wire up")
		c.Flags().BoolVar(&noGit, "no-git", false, "skip the git commit-msg hook")
	}
	install.Flags().BoolVar(&noDocs, "no-docs", false, "don't touch AGENTS.md / CLAUDE.md")
	cmd.AddCommand(install, uninstall, status)
	return cmd
}
