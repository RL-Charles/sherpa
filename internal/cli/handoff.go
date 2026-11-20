package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/RL-Charles/sherpa/internal/gitinfo"
	"github.com/RL-Charles/sherpa/internal/journal"
	"github.com/RL-Charles/sherpa/internal/model"
)

func (a *app) handoffCmd() *cobra.Command {
	var id, nowText, nextText, blocker string
	var clearBlocker bool
	var files []string
	cmd := &cobra.Command{
		Use:   "handoff",
		Short: "Record a concise state + next step so humans and the next session can resume cleanly",
		Long: `Update a ticket's Now line, next step, blocker and session/branch context in
one command, and append a handoff entry to the worklog.

  shp handoff --now "Parser wired; 2 edge cases left" \
    --next "Fix nested tables in internal/tui/table.go" \
    -f internal/tui/table.go

  shp handoff --now "Waiting on R350 slot" --blocker "R350 queue busy"`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.load(); err != nil {
				return err
			}
			it, err := a.resolve([]string{id})
			if err != nil {
				return err
			}
			nowText = strings.TrimSpace(nowText)
			nextText = strings.TrimSpace(nextText)
			blocker = strings.TrimSpace(blocker)
			if nowText == "" && nextText == "" && blocker == "" && !clearBlocker {
				return errors.New("give at least one of --now, --next, --blocker or --clear-blocker")
			}
			if nowText != "" {
				it.Now = nowText
			}
			if it.Handoff == nil {
				it.Handoff = &model.Handoff{}
			}
			if nextText != "" {
				it.Handoff.Next = nextText
			}
			if clearBlocker {
				it.Handoff.Blocker = ""
			} else if blocker != "" {
				it.Handoff.Blocker = blocker
			}
			it.Handoff.Agent = a.actor.Name
			it.Handoff.SessionID = agentSessionID()
			it.Handoff.Worktree = a.store.CheckoutKey()
			it.Handoff.Branch = gitinfo.Branch(a.store.CodeRoot)
			it.Handoff.Commit = gitinfo.Head(a.store.CodeRoot)
			it.Handoff.At = time.Now().UTC().Truncate(time.Second)

			base, _, _ := a.workSession(it.ID)
			added, removed := gitinfo.DiffStat(a.store.CodeRoot, base, files)
			var detail []string
			if it.Handoff.Next != "" {
				detail = append(detail, "next: "+it.Handoff.Next)
			}
			if it.Handoff.Blocker != "" {
				detail = append(detail, "blocker: "+it.Handoff.Blocker)
			}
			msg := nowText
			if msg == "" && nextText != "" {
				msg = "next: " + nextText
			} else if msg == "" && blocker != "" {
				msg = "blocked: " + blocker
			} else if msg == "" {
				msg = "cleared blocker"
			}
			if err := a.store.Save(it); err != nil {
				return err
			}
			e := journal.Event{
				Item:    it.ID,
				Type:    "handoff",
				Message: msg,
				Detail:  detail,
				Files:   files,
				Commit:  it.Handoff.Commit,
				Added:   added,
				Removed: removed,
			}
			if err := a.record(e); err != nil {
				return err
			}
			if !a.json && len([]rune(it.Now)) > 140 {
				fmt.Fprintf(os.Stderr, "hint: Now is %d chars; keep it ≤140 chars so it stays scannable in the TUI\n", len([]rune(it.Now)))
			}
			return a.done(it)
		},
	}
	a.itemFlag(cmd, &id)
	cmd.Flags().StringVar(&nowText, "now", "", "one-line current state (≤140 chars)")
	cmd.Flags().StringVar(&nextText, "next", "", "concrete next action for the resuming session")
	cmd.Flags().StringVar(&blocker, "blocker", "", "what is currently blocking progress")
	cmd.Flags().BoolVar(&clearBlocker, "clear-blocker", false, "clear any recorded handoff blocker")
	cmd.Flags().StringSliceVarP(&files, "file", "f", nil, "files touched (repeatable)")
	return cmd
}

// agentSessionID returns the native coding-agent session identifier when
// exposed by the agent's hook or environment.
func agentSessionID() string {
	for _, k := range []string{"SHERPA_AGENT_SESSION", "OPENCODE_SESSION_ID", "CLAUDE_SESSION_ID", "CODEX_SESSION_ID"} {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	return ""
}
