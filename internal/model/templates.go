package model

import (
	"fmt"
	"strings"
)

// TemplateNames is the set accepted by `shp new ticket --template`.
var TemplateNames = []string{"feature", "bug", "spike", "chore"}

// TemplateBody returns the markdown skeleton for a ticket template. The
// headings are the contract: Why/Context/Design on a feature, Expected/Actual/
// Repro on a bug, and so on. `shp lint` checks the same names.
func TemplateBody(name string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "feature":
		return `## Why

_What hurts today, for whom, and what changes if we ship this?_

## Context

- Background:
- Constraints:
- Prior art / links:
- Related tickets:

## Design

### Shape

_One paragraph: the approach, and why it is the simplest thing that works._

### Types and enums

_Show the new or changed types and enum values, with what each value means._

~~~go
// Example shape: keep only the types that prove the design.
// type Status string
// const (
//     StatusPlanned Status = "planned"
//     StatusTouched Status = "touched"
//     StatusDone    Status = "done"
// )
~~~

### Pattern

_Name the pattern (adapter, strategy, state machine, event, …) and why it fits._

### Invariants

- _What must always be true?_

### Contracts

- Inputs / outputs:
- Events / side effects:
- Errors:

### Edge cases

- _Empty, expired, concurrent, partial failure, permissions…_

### Alternatives

- _What did you consider and reject, and why?_

## Out of scope

- …

## Risks

- …
`, nil
	case "bug":
		return `## Expected

_What should happen?_

## Actual

_What happens instead? Include the exact error or symptom._

## Repro

1. …
2. …

## Context

- Affected versions / environments:
- First seen:
- Related tickets:

## Design

### Suspected cause

_Which code path, invariant or assumption is wrong?_

### Types and enums

_Only if the fix changes a type, enum, payload or error code._

~~~go
// type / enum sketch
~~~

### Regression test

_How will this be proven not to come back?_

## Out of scope

- …

## Risks

- …
`, nil
	case "spike":
		return `## Question

_What decision should this spike unblock?_

## Timebox

_How long, and what is the deadline?_

## Context

- Constraints:
- Prior art / links:
- Related tickets:

## What I tried

- …

## Findings

_What did we learn? Link evidence, benchmarks or prototypes._

## Recommendation

_What should we do next, and what would change your mind?_

## Out of scope

- …
`, nil
	case "chore":
		return `## Why

_What maintenance or operational need does this serve?_

## Change

_What changes, and what deliberately does not?_

## Design notes

- Types / enums / contracts affected:
- Invariants:
- Edge cases:

## Verification

_How will we know it worked?_

## Blast radius

_What could break, and how do we roll it back?_

## Out of scope

- …
`, nil
	}
	return "", fmt.Errorf("unknown template %q (want %s)", name, strings.Join(TemplateNames, ", "))
}

// FillTemplate replaces the first standalone italic placeholder in a template
// with description, so `-m` still works when a template is chosen.
func FillTemplate(body, description string) string {
	description = strings.TrimSpace(description)
	if description == "" {
		return body
	}
	lines := strings.Split(body, "\n")
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if len(t) >= 2 && strings.HasPrefix(t, "_") && strings.HasSuffix(t, "_") && !strings.Contains(t[1:len(t)-1], "_") {
			lines[i] = description
			break
		}
	}
	return strings.Join(lines, "\n")
}
