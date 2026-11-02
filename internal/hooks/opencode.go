package hooks

import (
	"encoding/json"
	"fmt"
	"io"
)

// opencode adapts the Sherpa OpenCode plugin (.opencode/plugins/sherpa.ts).
// The plugin passes a small JSON payload; Sherpa answers with plain text on
// stdout and exit code 2 when the plugin should block or continue.
// The plugin targets the OpenCode V2 API; V1 plugins do not load in V2.
// https://opencode.ai/v2/docs/build/plugins/server/
type opencode struct{}

func init() { Adapters["opencode"] = opencode{} }

func (opencode) Parse(ev Event, payload []byte) Input {
	var p struct {
		Dir        string   `json:"dir"`
		Files      []string `json:"files"`
		StopActive bool     `json:"stopActive"`
		Command    string   `json:"command"`
		Output     string   `json:"output"`
	}
	_ = json.Unmarshal(payload, &p)
	return Input{Dir: p.Dir, Files: p.Files, StopActive: p.StopActive, Command: p.Command, Output: p.Output}
}

func (opencode) Write(ev Event, out Output, stdout, _ io.Writer) int {
	switch {
	case ev == SessionStart && out.Context != "":
		fmt.Fprint(stdout, out.Context)
	case out.Block:
		fmt.Fprint(stdout, out.Reason)
		return 2
	}
	return 0
}
