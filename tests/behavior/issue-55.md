# Issue #55: The handoff commit can hang unattended on a signing key

When a planning Agent leaves the handoff uncommitted, Owl commits it itself
(ADR-0026). Reported at commit `701b936`: that commit ran with the user's own
signing configuration in effect and under no deadline. A user with
`commit.gpgsign` on and a key behind a passphrase hung the daemon on a prompt
nobody was there to answer, while the rebase before every Run already turns
signing off for exactly that reason.

The fix is that the handoff commit turns signing off, as the rebase does, and
runs under a context the daemon's stopping cuts short. Signing Owl's commits
with a key of its own is out of scope.

The git scenarios exercise that package's own API with a program standing in
for the signing tool. The end-to-end scenario drives the built `owl` binary
against a daemon, with the stub agent of issue #5 standing in for Claude Code
and leaving the handoff uncommitted, on a Project whose repository asks for
every commit to be signed.

## Scenarios

### S1 - the handoff commit does not invoke the signing program
Given a repository with `commit.gpgsign` on and `gpg.program` naming a program that records it was run and then fails
When Owl commits a path in it
Then the commit is made
And the program was never run

### S2 - cancelling the context ends a commit that is stuck
Given a repository whose post-commit hook waits for a minute
When Owl commits a path in it and the context is cancelled a moment later
Then the commit call returns within the kill delay plus a margin
And it returns an error rather than success

### S3 - the same for a repository that signs with ssh
Given a repository with `commit.gpgsign` on, `gpg.format` set to `ssh`, and `gpg.ssh.program` naming a program that records it was run and then fails
When Owl commits a path in it
Then the commit is made
And the program was never run

### S4 - a planning Run commits the handoff in a Project that asks for signed commits
Given a running daemon, a Project whose repository has `commit.gpgsign` on and `gpg.program` naming a program that records it was run and then fails, and a planned Job whose Agent writes the handoff and leaves it uncommitted
When the planning Run ends
Then the handoff is committed on the Job's branch
And the Job is `pending` again for its execution Run
And the program was never run

### S5 - a daemon stop that cuts the handoff commit short interrupts the Run rather than failing it
Given a planned Job whose Agent left the handoff uncommitted, in a repository whose post-commit hook waits for a minute
When the daemon stops while Owl is committing the handoff
Then the Run is recorded `interrupted`, not `failed`
And the Job is `pending` again rather than `blocked`

### S6 - a handoff commit slower than the bookkeeping budget still records the plan
Given a planned Job whose Agent left the handoff uncommitted, in a repository whose post-commit hook takes longer than the daemon's bookkeeping deadline
When the planning Run ends
Then the Run is recorded `succeeded`
And the Job is `pending` again with its plan recorded
