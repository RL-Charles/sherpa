package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/RL-Charles/sherpa/internal/gitinfo"
	"github.com/RL-Charles/sherpa/internal/journal"
	"github.com/RL-Charles/sherpa/internal/model"
	"github.com/RL-Charles/sherpa/internal/policy"
	"github.com/RL-Charles/sherpa/internal/render"
	"github.com/RL-Charles/sherpa/internal/review"
	"github.com/RL-Charles/sherpa/internal/store"
	"github.com/RL-Charles/sherpa/internal/workflow"
)

// launchReviewer starts the configured reviewer agent on it and records that
// in the journal.
func (a *app) launchReviewer(it *model.Item) error {
	log, err := review.Spawn(a.store, a.store.Config.Reviewer.Command, it.ID)
	if err != nil {
		return err
	}
	if err := a.record(journal.Event{Item: it.ID, Type: "review", Kind: "spawn",
		Message: a.store.Config.Reviewer.Command, Detail: []string{"log: " + log}}); err != nil {
		return err
	}
	if !a.json {
		fmt.Fprintf(os.Stderr, "⟳ independent reviewer launched for %s (log: %s)\n", it.ID, log)
	}
	return nil
}

// autoReview launches the reviewer after an agent submit when the project
// asks for it. Reviewers never launch reviewers. Failure only warns: the
// work is submitted either way.
func (a *app) autoReview(it *model.Item) {
	r := a.store.Config.Reviewer
	if !r.Auto || r.Command == "" || os.Getenv("SHERPA_REVIEW") != "" {
		return
	}
	if err := a.launchReviewer(it); err != nil {
		fmt.Fprintln(os.Stderr, "⚠ reviewer not launched:", err)
	}
}

// selfReview refuses a verdict from the session that submitted the work.
func (a *app) selfReview(id string) error {
	events, err := journal.ReadAll(a.store.Dir)
	if err != nil {
		return err
	}
	author := ""
	for _, e := range journal.ForItem(events, id) {
		if e.Type == "submit" {
			author = e.Session
		}
	}
	if author != "" && author == a.session {
		return fmt.Errorf("this session submitted %s, so it can't review it: run `shp review %s --spawn` for an independent reviewer, or leave it for a human", id, id)
	}
	return nil
}

// submitted is what the item's last submit changed, and its summary.
func (a *app) submitted(id string) (policy.Change, string) {
	events, err := journal.ReadAll(a.store.Dir)
	if err != nil {
		return policy.Change{}, ""
	}
	var ch policy.Change
	summary := ""
	for _, e := range journal.ForItem(events, id) {
		if e.Type == "submit" {
			ch = policy.Change{Lines: e.Added + e.Removed, Files: e.Files}
			summary = e.Message
		}
	}
	return ch, summary
}

// needsHuman returns why a human must close it under the project's policy,
// or "" when a passing agent review may close it.
func (a *app) needsHuman(p policy.Policy, it *model.Item) (string, error) {
	all, err := a.store.All()
	if err != nil {
		return "", err
	}
	ch, _ := a.submitted(it.ID)
	return p.Human(it, all, ch), nil
}

// humanRule returns the human_review rule that keeps an agent from closing
// it, as "rule: why", or "".
func (a *app) humanRule(p policy.Policy, it *model.Item) (string, error) {
	all, err := a.store.All()
	if err != nil {
		return "", err
	}
	ch, _ := a.submitted(it.ID)
	if name, why := p.Rule(it, all, ch); name != "" {
		return name + ": " + why, nil
	}
	return "", nil
}

// afterAgentClose runs the policy's steps for a ticket an agent just closed:
// commit its submitted files when commit is auto, and flag it for a human
// audit when sampled. Failures only warn; the ticket stays closed.
func (a *app) afterAgentClose(p policy.Policy, it *model.Item) {
	if p.Commit == policy.CommitAuto {
		ch, summary := a.submitted(it.ID)
		if summary == "" {
			summary = it.Title
		}
		files := ch.Files
		if a.store.CodeRoot == a.store.Root {
			files = append(slices.Clone(files), filepath.ToSlash(filepath.Join(store.DirName, it.Kind.Dir(), it.ID+".md")))
		}
		sha, err := gitinfo.Commit(a.store.CodeRoot, summary+"\n\nTicket: "+it.ID, files)
		switch {
		case err != nil:
			fmt.Fprintln(os.Stderr, "⚠ policy commit failed:", err)
		case sha != "" && !a.json:
			fmt.Fprintf(os.Stderr, "✔ committed %s as %s (policy commit: auto)\n", it.ID, sha)
		}
	}
	if p.Sampled(it.ID) {
		if err := a.record(journal.Event{Item: it.ID, Type: "audit", Kind: "sampled",
			Message: fmt.Sprintf("sampled for a human audit (sample %g)", p.Sample)}); err != nil {
			fmt.Fprintln(os.Stderr, "⚠ audit flag not recorded:", err)
		} else if !a.json {
			fmt.Fprintf(os.Stderr, "⚑ %s sampled for a human audit (shp policy audit)\n", it.ID)
		}
	}
}

// pendingAudits returns the sampled items no human has audited yet, oldest first.
func pendingAudits(events []journal.Event) []string {
	var ids []string
	for _, e := range events {
		if e.Type != "audit" {
			continue
		}
		i := slices.Index(ids, e.Item)
		switch {
		case e.Kind == "sampled" && i < 0:
			ids = append(ids, e.Item)
		case e.Kind != "sampled" && i >= 0:
			ids = slices.Delete(ids, i, i+1)
		}
	}
	return ids
}

func (a *app) policyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "policy [id]",
		Short: "Show which work waits for a human, or whether one item does",
		Long: `Tickets close on a passing agent review. .sherpa/policy.yaml names the
exceptions, which wait for a human, and what happens when an agent closes one:

  human_review:
    - name: migrations
      paths: [migrations/**]  # its submit or code map touches one of these
    - name: flagged
      labels: [security]      # it has any of these labels
    - name: big-tooling
      under: [S-008]          # it sits under this story or epic
      min_lines: 400          # and its submit changed at least this many lines
  commit: auto                # commit its submitted files when an agent closes it
  sample: 0.1                 # share of auto-closed tickets flagged for a human audit
  verify: go test ./...       # what the reviewer runs before passing work

Stories and epics always close by a human, and so does a ticket with an
unresolved deliverable or check. Draft a policy with shp policy init.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.load(); err != nil {
				return err
			}
			p, err := policy.Load(a.store.Dir)
			if err != nil {
				return err
			}
			if len(args) == 0 {
				if a.json {
					return a.emit(p)
				}
				if !p.Found {
					fmt.Println("No review policy: every ticket closes on a passing agent review. Draft one with `shp policy init`.")
					return nil
				}
				printPolicy(p)
				return nil
			}
			it, err := a.store.Get(args[0])
			if err != nil {
				return err
			}
			why, err := a.needsHuman(p, it)
			if err != nil {
				return err
			}
			if a.json {
				return a.emit(map[string]any{"id": it.ID, "human": why != "", "why": why})
			}
			if why == "" {
				fmt.Printf("%s: a passing agent review closes it\n", it.ID)
			} else {
				fmt.Printf("%s: waits for a human (%s)\n", it.ID, why)
			}
			return nil
		},
	}
	cmd.AddCommand(a.policyInitCmd(), a.policyAuditCmd())
	return cmd
}

func printPolicy(p policy.Policy) {
	if len(p.HumanReview) == 0 {
		fmt.Println("human_review: none (every ready ticket closes on a passing agent review)")
	}
	for _, r := range p.HumanReview {
		var when []string
		if len(r.Paths) > 0 {
			when = append(when, "paths "+strings.Join(r.Paths, "|"))
		}
		if len(r.Labels) > 0 {
			when = append(when, "labels "+strings.Join(r.Labels, "|"))
		}
		if len(r.Under) > 0 {
			when = append(when, "under "+strings.Join(r.Under, "|"))
		}
		if r.MinLines > 0 {
			when = append(when, fmt.Sprintf("≥ %d lines", r.MinLines))
		}
		fmt.Printf("%-14s %s\n", r.Name, strings.Join(when, " · "))
	}
	fmt.Printf("commit: %s · sample: %g", p.Commit, p.Sample)
	if p.Verify != "" {
		fmt.Printf(" · verify: %s", p.Verify)
	}
	fmt.Println()
}

func (a *app) policyInitCmd() *cobra.Command {
	var commit, verify, reviewer string
	var sample float64
	var autoReview, force, dryRun bool
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Draft .sherpa/policy.yaml from the repo and preview it on recent tickets",
		Long: `Scan the checkout for work that is hard to revert or reaches beyond the
repo (migrations, CI and deploy config, dependency manifests, auth and
secrets) and draft human_review rules for it, plus "flagged" (labels
security, needs-human) and "large" (400+ lines). The verify command is
guessed from the repo unless given.

Agents: settle these with the user first, then pass them as flags:
which paths need a human, --commit auto|off, --sample, --verify, and
optionally a --reviewer command (e.g. "claude -p") with --auto-review.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.load(); err != nil {
				return err
			}
			path := filepath.Join(a.store.Dir, policy.File)
			if _, err := os.Stat(path); err == nil && !force && !dryRun {
				return fmt.Errorf("%s exists; edit it, or pass --force to redraft it", path)
			}
			p := policy.Draft(a.store.CodeRoot)
			if cmd.Flags().Changed("verify") {
				p.Verify = verify
			}
			p.Commit, p.Sample = commit, sample
			text := policy.Render(p)
			check := filepath.Join(os.TempDir(), fmt.Sprintf("sherpa-policy-%d", os.Getpid()))
			if err := os.MkdirAll(check, 0o755); err != nil {
				return err
			}
			defer os.RemoveAll(check)
			if err := os.WriteFile(filepath.Join(check, policy.File), []byte(text), 0o644); err != nil {
				return err
			}
			p, err := policy.Load(check)
			if err != nil {
				return err
			}
			if dryRun {
				fmt.Print(text)
			} else {
				if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
					return err
				}
				fmt.Println("✔ wrote", filepath.Join(store.DirName, policy.File))
				if reviewer != "" {
					a.store.Config.Reviewer = store.Reviewer{Command: reviewer, Auto: autoReview}
					data, err := yaml.Marshal(a.store.Config)
					if err != nil {
						return err
					}
					if err := os.WriteFile(filepath.Join(a.store.Dir, "config.yaml"), data, 0o644); err != nil {
						return err
					}
					fmt.Printf("✔ reviewer: %s (auto: %v)\n", reviewer, autoReview)
				}
			}
			fmt.Println()
			printPolicy(p)
			return a.previewPolicy(p)
		},
	}
	cmd.Flags().StringVar(&commit, "commit", policy.CommitOff, "auto: commit a ticket's files when an agent closes it; off: leave commits to you")
	cmd.Flags().Float64Var(&sample, "sample", 0.1, "share of auto-closed tickets flagged for a human audit (0-1)")
	cmd.Flags().StringVar(&verify, "verify", "", "command the reviewer runs (default: guessed from the repo)")
	cmd.Flags().StringVar(&reviewer, "reviewer", "", "reviewer agent command for config.yaml, e.g. \"claude -p\"")
	cmd.Flags().BoolVar(&autoReview, "auto-review", false, "launch the reviewer after every agent submit")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite an existing policy.yaml")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print the draft without writing it")
	return cmd
}

// previewPolicy shows how p would treat the most recent submitted tickets.
func (a *app) previewPolicy(p policy.Policy) error {
	all, err := a.store.All()
	if err != nil {
		return err
	}
	var recent []*model.Item
	for _, it := range all {
		if it.Kind == model.Ticket && (it.Status == model.Done || it.Status == model.Review || it.Status == model.AgentReview) {
			recent = append(recent, it)
		}
	}
	if len(recent) == 0 {
		return nil
	}
	if len(recent) > 8 {
		recent = recent[len(recent)-8:]
	}
	fmt.Println("\nOn recent tickets:")
	for _, it := range recent {
		ch, _ := a.submitted(it.ID)
		verdict := "✅ auto-close"
		if why := p.Human(it, all, ch); why != "" {
			verdict = "✋ human — " + why
		}
		fmt.Printf("  %s %-40.40s %s\n", it.ID, it.Title, verdict)
	}
	return nil
}

func (a *app) policyAuditCmd() *cobra.Command {
	var notes string
	var reopen bool
	cmd := &cobra.Command{
		Use:   "audit [id]",
		Short: "List auto-closed tickets sampled for a human audit, or record one",
		Long: `The policy's sample flags a share of the tickets agents close. A human
reads each one after it lands and records the audit:

  shp policy audit                    # what is waiting
  shp policy audit T-0042 -m "fine"   # audited, nothing to change
  shp policy audit T-0042 --reopen -m "missed the empty case"

Recurring findings belong in policy.yaml, a lint rule or a test, not in
another round of review.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.load(); err != nil {
				return err
			}
			events, err := journal.ReadAll(a.store.Dir)
			if err != nil {
				return err
			}
			pending := pendingAudits(events)
			if len(args) == 0 {
				if a.json {
					return a.emit(pending)
				}
				if len(pending) == 0 {
					fmt.Println("No sampled tickets waiting for an audit.")
				}
				for _, id := range pending {
					if it, err := a.store.Get(id); err == nil {
						fmt.Println(render.Line(it))
					}
				}
				return nil
			}
			if a.actor.Kind == workflow.Agent {
				return errors.New("audits are for humans: they check what agents closed on their own")
			}
			if strings.TrimSpace(notes) == "" {
				return errors.New("audit notes are required (-m \"…\")")
			}
			it, err := a.store.Get(args[0])
			if err != nil {
				return err
			}
			kind := "ok"
			if reopen {
				kind = "reopen"
				if _, err := a.setStatus(it, model.InProgress); err != nil {
					return err
				}
			}
			if err := a.record(journal.Event{Item: it.ID, Type: "audit", Kind: kind, Message: notes,
				To: string(it.Status), Commit: gitinfo.Head(a.store.CodeRoot)}); err != nil {
				return err
			}
			return a.done(it)
		},
	}
	cmd.Flags().StringVarP(&notes, "message", "m", "", "what you checked (required)")
	cmd.Flags().BoolVar(&reopen, "reopen", false, "send the ticket back to in progress")
	return cmd
}
