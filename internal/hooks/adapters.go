package hooks

import "io"

// Adapter translates one agent's hook payload into an Input, and an Output
// into that agent's response. Write returns the process exit code.
type Adapter interface {
	Parse(ev Event, payload []byte) Input
	Write(ev Event, out Output, stdout, stderr io.Writer) int
}

// Adapters by agent name.
var Adapters = map[string]Adapter{}
