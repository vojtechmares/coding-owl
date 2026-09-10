package run

import "strings"

// Contract is Owl's standing unattended contract (ADR-0017). Every Run carries
// it, so a Job inherits sane unattended behaviour without the user restating
// it. It is short and stable on purpose: it is a contract, not a style guide,
// and changing it changes behaviour in every Project at once.
var Contract = `You are running unattended, as part of Coding Owl. Nobody is watching you and nobody can answer a question, so:

- Make reasonable assumptions rather than stalling, and write down what you assumed and why.
- Commit incrementally, with messages that explain the reasoning rather than restating the diff.
- Keep ` + HandoffPath + ` current as you go, not at the end: the next run starts with no memory of this one, and reads that file to find out where you got to.
- When you are genuinely blocked, stop and state the blocker plainly rather than working around it.
- Never guess at anything destructive or irreversible.`

// clausesHeading introduces the Project's own clauses, so a reader can tell
// what Owl asks of every Job from what this Project asks of its own.
const clausesHeading = "This project also asks that you:"

// SystemPrompt is the effective system prompt for a Run: the standing contract
// with the Project's clauses after it. A Project may add to the contract; it
// cannot replace it, and because Project configuration is read from the base
// branch (ADR-0014), an Agent cannot weaken the contract it is running under.
func SystemPrompt(clauses []string) string {
	kept := make([]string, 0, len(clauses))
	for _, c := range clauses {
		if c := strings.TrimSpace(c); c != "" {
			kept = append(kept, "- "+c)
		}
	}
	if len(kept) == 0 {
		return Contract
	}
	return Contract + "\n\n" + clausesHeading + "\n\n" + strings.Join(kept, "\n")
}
