package agent

import (
	"fmt"
	"strings"

	"github.com/vojtechmares/coding-owl/internal/verifier"
)

// systemPrompt is what the reviewer is for, and what it is not for. It is
// Owl's own contract with a reviewer, not the Project's: a Project's
// unattended clauses were written for an Agent that changes things (ADR-0017),
// and this one must not.
const systemPrompt = `You are reviewing somebody else's work, as a second pair of eyes that did not do it.

- Read what you are given and judge whether the work does what was asked, correctly and safely.
- Change nothing. Do not edit files, do not commit, do not run anything that writes. Reading the repository and running read-only commands is fine.
- Say what is wrong, with the file and the line, and why it matters. A finding nobody can act on is not a finding.
- Being unable to tell is a finding of its own, not a pass.
- Write your verdict exactly where you are asked to, and write nothing else anywhere.`

// prompt is what the reviewer is given: the plan the work was meant to carry
// out, the diff it produced, and where to leave its verdict. Nothing of the
// Session that produced the work goes in, which is the whole point (ADR-0013).
func prompt(req verifier.Request) string {
	var b strings.Builder
	b.WriteString("Review the change on this branch.\n\n")

	if plan := strings.TrimSpace(req.Plan); plan != "" {
		b.WriteString("## What was planned\n\n")
		b.WriteString(plan)
		b.WriteString("\n\n")
	}

	b.WriteString("## What changed\n\n")
	diff := strings.TrimSpace(req.Diff)
	switch {
	case diff == "":
		b.WriteString("Nothing: the branch changed no file. Read the repository and say whether that is right.\n\n")
	case req.DiffComplete:
		b.WriteString("```diff\n")
		b.WriteString(diff)
		b.WriteString("\n```\n\n")
	default:
		b.WriteString("This is the beginning of the diff; it was too long to carry whole, so run `git diff` in this repository to read the rest.\n\n")
		b.WriteString("```diff\n")
		b.WriteString(diff)
		b.WriteString("\n```\n\n")
	}

	b.WriteString(verdictInstruction())
	return b.String()
}

// verdictInstruction is how a reviewer answers: one file, with the verdict on
// its first line, which is the only thing Owl reads out of this Session.
func verdictInstruction() string {
	return fmt.Sprintf(`## How to answer

Write your verdict to %s in this directory, and change nothing else.

Its first line must be exactly one of:

    verdict: %s
    verdict: %s

Then, under that line, your findings: what is wrong, where, and why it matters.
Write %s when the work does what was asked and you found nothing that should
stop it. A file with no verdict line is read as no answer at all.
`, VerdictPath, verdictPass, verdictFail, verdictPass)
}
