package hooks

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/RL-Charles/sherpa/internal/store"
)

var (
	reTicketTrailer = regexp.MustCompile(`(?m)^Ticket:\s*T-\d+`)
	reTrailer       = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9-]*: .+`)
)

// CommitMsg is the git commit-msg hook. A message that already names a ticket
// passes; otherwise the ticket claimed in this checkout is added as a
// `Ticket:` trailer. With no ticket to add, agents are refused and humans
// pass. Merges, reverts and fixups always pass.
func CommitMsg(dir, path string, agent bool) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	msg := string(data)
	var body []string
	for _, l := range strings.Split(msg, "\n") {
		if !strings.HasPrefix(l, "#") {
			body = append(body, l)
		}
	}
	text := strings.TrimSpace(strings.Join(body, "\n"))
	first, _, _ := strings.Cut(text, "\n")
	for _, p := range []string{"Merge ", "Revert ", "fixup!", "squash!", "amend!"} {
		if strings.HasPrefix(first, p) {
			return nil
		}
	}
	if text == "" || reTicketTrailer.MatchString(text) {
		return nil
	}
	s, err := store.Find(dir)
	if err != nil {
		return nil
	}
	if it := activeTicket(s); it != nil {
		return os.WriteFile(path, []byte(addTrailer(msg, "Ticket: "+it.ID)), 0o644)
	}
	if !agent {
		return nil
	}
	return fmt.Errorf("sherpa: commit blocked. Agent commits must name their ticket: claim one with `shp start <T-id>` " +
		"(the Ticket: trailer is then added automatically) or add a `Ticket: T-xxxx` line yourself")
}

// addTrailer appends a trailer to the message, joining an existing trailer
// block (e.g. Co-Authored-By) and keeping git's # comment lines last.
func addTrailer(msg, trailer string) string {
	lines := strings.Split(strings.TrimRight(msg, "\n"), "\n")
	end := len(lines) // first index of the trailing comment block
	for end > 0 && strings.HasPrefix(lines[end-1], "#") {
		end--
	}
	content := append([]string(nil), lines[:end]...)
	comments := lines[end:]
	for len(content) > 0 && strings.TrimSpace(content[len(content)-1]) == "" {
		content = content[:len(content)-1]
	}
	// Join an existing trailer block only if it follows a blank line;
	// otherwise start one. (A lone subject like "docs: x" is not a trailer.)
	k := len(content)
	for k > 0 && reTrailer.MatchString(content[k-1]) {
		k--
	}
	if k == len(content) || k == 0 || strings.TrimSpace(content[k-1]) != "" {
		content = append(content, "")
	}
	out := append(content, trailer)
	if len(comments) > 0 {
		out = append(append(out, ""), comments...)
	}
	return strings.Join(out, "\n") + "\n"
}
