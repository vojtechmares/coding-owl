# Issue #49: A Run hangs forever after one over-long stdout line

The daemon reads an Agent's structured output one line at a time, and refuses a
line longer than 8 MiB as a read error. Reported at commit `701b936`: when that
happens the read loop stops, nothing reads the pipe any more, and an Agent that
still has more than the pipe's buffer to say blocks in `write(2)` forever. The
Run never ends and holds its Job.

The scenarios drive the built `owl` binary against a daemon, with the stub agent
of issue #5 standing in for Claude Code. The stub learns one script directive
for this: a line reading `#fill <n>` emits a single line of `n` bytes, so a
script can say more than a scanner will read without a multi-megabyte script
file on disk.

"Over-long" below means longer than the 8 MiB the daemon reads a line up to.
"More output than a pipe holds" means more than 64 KiB, which is the largest a
pipe buffers on the platforms Owl runs on, so an Agent that writes that much
into a pipe nobody reads is blocked for good.

## Scenarios

### S1 - a Run ends after an over-long line followed by more output than a pipe holds
Given a running daemon, a registered Project, and a pending Job whose Agent emits a normal line, then one over-long line, then more output than a pipe holds, then exits 0
When the Job is run with `owl start`
Then within twenty seconds `owl jobs show` reports the Job as `blocked` with one Run whose outcome is `failed` and whose ENDED column is set

### S2 - the over-long line is the Run's recorded reason
Given the Job of S1 has ended
When `owl jobs show` is read
Then its reason says the agent's output could not be read and names the line as too long

### S3 - what the Agent said before the over-long line is still in the log
Given the Job of S1 has ended
When `owl logs <run>` is read
Then it prints the normal line the Agent emitted before the over-long one
