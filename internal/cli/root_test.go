package cli

import "testing"

func TestCommandName(t *testing.T) {
	for argv0, want := range map[string]string{
		"/home/u/.local/bin/sherpa": "sherpa", "sherpa.exe": "sherpa",
		"/usr/bin/shp": "shp", "bin/shp": "shp", "something-else": "shp",
	} {
		if got := CommandName(argv0); got != want {
			t.Errorf("CommandName(%q) = %q, want %q", argv0, got, want)
		}
	}
}
