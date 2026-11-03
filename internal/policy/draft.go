package policy

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Draft proposes a policy for the checkout at root: human_review rules for
// the risky paths it finds (work that is hard to revert or reaches beyond
// the repo), plus a guessed verify command. Nothing is written.
func Draft(root string) Policy {
	var migrations, ship, deps, secrets []string
	add := func(list *[]string, g string) {
		if !slices.Contains(*list, g) {
			*list = append(*list, g)
		}
	}
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		name := d.Name()
		if d.IsDir() {
			if rel != "." && (strings.Count(rel, "/") >= 4 || skipDir[name]) {
				return filepath.SkipDir
			}
			switch {
			case name == "migrations" || name == "migrate" || name == "alembic":
				add(&migrations, rel+"/**")
			case rel == ".github/workflows" || name == ".circleci" || name == "infra" || name == "terraform" ||
				name == "deploy" || name == "k8s" || name == "helm":
				add(&ship, rel+"/**")
			case name == "auth" || name == "security" || name == "crypto":
				add(&secrets, "**/"+name+"/**")
			}
			return nil
		}
		switch {
		case depFiles[name]:
			add(&deps, name)
		case name == "Dockerfile" || strings.HasPrefix(name, "docker-compose"):
			add(&ship, name)
		case strings.HasPrefix(name, ".env"):
			add(&secrets, ".env*")
		}
		return nil
	})
	var p Policy
	rule := func(name string, paths []string) {
		if len(paths) > 0 {
			slices.Sort(paths)
			p.HumanReview = append(p.HumanReview, Rule{Name: name, Paths: paths})
		}
	}
	rule("migrations", migrations)
	rule("ship", ship)
	rule("dependencies", deps)
	rule("secrets", secrets)
	p.HumanReview = append(p.HumanReview,
		Rule{Name: "flagged", Labels: []string{"security", "needs-human"}},
		Rule{Name: "large", MinLines: 400})
	p.Commit = CommitOff
	p.Verify = GuessVerify(root)
	return p
}

var skipDir = map[string]bool{
	".git": true, ".sherpa": true, "node_modules": true, "vendor": true, "dist": true,
	"build": true, "target": true, ".venv": true, "venv": true, "bin": true,
}

var depFiles = map[string]bool{
	"go.mod": true, "package.json": true, "Cargo.toml": true, "pyproject.toml": true,
	"requirements.txt": true, "Gemfile": true, "composer.json": true,
}

// GuessVerify returns the test command the checkout most likely uses, or "".
func GuessVerify(root string) string {
	has := func(f string) bool { _, err := os.Stat(filepath.Join(root, f)); return err == nil }
	read := func(f string) string { b, _ := os.ReadFile(filepath.Join(root, f)); return string(b) }
	cmd := ""
	switch {
	case has("go.mod"):
		cmd = "go test ./..."
	case has("Cargo.toml"):
		cmd = "cargo test"
	case has("package.json") && strings.Contains(read("package.json"), `"test"`):
		cmd = "npm test"
	case has("pyproject.toml") || has("pytest.ini"):
		cmd = "pytest"
	case strings.Contains(read("Makefile"), "\ntest:") || strings.HasPrefix(read("Makefile"), "test:"):
		return "make test"
	default:
		return ""
	}
	if has("mise.toml") || has(".mise.toml") {
		cmd = "mise exec -- " + cmd
	}
	return cmd
}

// Render writes p as a commented policy.yaml.
func Render(p Policy) string {
	var b strings.Builder
	b.WriteString(`# Sherpa review policy. Tickets close on a passing agent review unless a
# human_review rule matches. Stories and epics always close by a human, and
# so does a ticket with an unresolved deliverable or check.
# Check one ticket with: shp policy <T-id>

# A ticket matching any rule waits for a human. Every condition a rule gives
# must hold (paths: its submit or code map touches one; labels: it has one;
# under: it sits below that story/epic; min_lines: its submit changed that many).
human_review:
`)
	if len(p.HumanReview) == 0 {
		b.WriteString("  []\n")
	}
	list := func(xs []string) string {
		q := make([]string, len(xs))
		for i, x := range xs {
			q[i] = fmt.Sprintf("%q", x)
		}
		return "[" + strings.Join(q, ", ") + "]"
	}
	for _, r := range p.HumanReview {
		fmt.Fprintf(&b, "  - name: %s\n", r.Name)
		if len(r.Paths) > 0 {
			fmt.Fprintf(&b, "    paths: %s\n", list(r.Paths))
		}
		if len(r.Labels) > 0 {
			fmt.Fprintf(&b, "    labels: %s\n", list(r.Labels))
		}
		if len(r.Under) > 0 {
			fmt.Fprintf(&b, "    under: %s\n", list(r.Under))
		}
		if r.MinLines > 0 {
			fmt.Fprintf(&b, "    min_lines: %d\n", r.MinLines)
		}
	}
	commit := p.Commit
	if commit == "" {
		commit = CommitOff
	}
	fmt.Fprintf(&b, "\n# auto: commit a ticket's submitted files (with a Ticket: trailer) when an agent closes it.\ncommit: %s\n", commit)
	fmt.Fprintf(&b, "\n# Share of auto-closed tickets flagged for a human audit (shp policy audit).\nsample: %g\n", p.Sample)
	if p.Verify != "" {
		fmt.Fprintf(&b, "\n# What the reviewer runs before passing work.\nverify: %q\n", p.Verify)
	} else {
		b.WriteString("\n# What the reviewer runs before passing work.\n# verify: \"make test\"\n")
	}
	return b.String()
}
