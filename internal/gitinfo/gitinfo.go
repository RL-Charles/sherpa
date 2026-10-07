// Package gitinfo reads just enough git state to tie worklog entries to code:
// the current commit and line counts for touched files.
package gitinfo

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

func git(root string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	return strings.TrimSpace(out.String()), err
}

// Head returns the short sha of HEAD, or "" outside git or before the first commit.
func Head(root string) string {
	sha, err := git(root, "rev-parse", "--short", "HEAD")
	if err != nil {
		return ""
	}
	return sha
}

// DiffStat counts lines added and removed in files since base (a commit),
// including uncommitted changes. With no base (no commits yet), or for files
// git does not track yet, every line counts as added. Unknown or deleted
// files are skipped.
func DiffStat(root, base string, files []string) (added, removed int) {
	if len(files) == 0 {
		return 0, 0
	}
	tracked := map[string]bool{}
	if base != "" {
		out, err := git(root, append([]string{"diff", "--numstat", base, "--"}, files...)...)
		if err == nil {
			for _, line := range strings.Split(out, "\n") {
				f := strings.Fields(line)
				if len(f) < 3 {
					continue
				}
				a, _ := strconv.Atoi(f[0]) // "-" for binary files → 0
				r, _ := strconv.Atoi(f[1])
				added += a
				removed += r
				tracked[f[2]] = true
			}
		}
	}
	for _, f := range files {
		if tracked[f] {
			continue
		}
		if base != "" {
			if _, err := git(root, "cat-file", "-e", base+":"+f); err == nil {
				continue // existed at base and unchanged
			}
		}
		data, err := os.ReadFile(filepath.Join(root, f))
		if err != nil {
			continue
		}
		added += bytes.Count(data, []byte("\n"))
	}
	return added, removed
}
