# Issue #21: Chat commands - argv allowlist gate, per-command argument rules, consent flow, daemon execve, working-directory confinement

The chat can look at the repository, and nothing can make it do more (ADR-0022).
A command is parsed into argv by the daemon, never by a shell; `argv[0]` must be
on the allowlist and its arguments must match that command's rules; the working
directory is a Project or one of its worktrees, chosen explicitly; every command
needs consent; and the daemon runs it itself.

## Scenarios

### S1 - An allowed command runs where it was asked for and its output reaches the chat
Given a Project is registered and a model asks to run `git status --short` in that Project's directory
When the chat asks for consent and the user allows it once
Then the app is told what will run, as the program and its arguments and the directory, before it runs
And the command's output is shown in the chat
And the model is given that same output and answers from it

### S2 - A program that is not on the allowlist is denied, and nobody is asked
Given a model asks to run `rm -rf .` in a Project's directory
When the answer streams
Then no consent is asked
And the model is told that `rm` is not a program Owl runs, naming what it does run
And nothing is executed

### S3 - The git that writes is denied, whether it is the subcommand or what follows it
Given a model asks in turn for `git checkout main`, `git reset --hard`, `git clean -fd`, `git push` and `git branch owl-new`
When each answer streams
Then each is denied without asking for consent, naming what was refused
And the Project's working tree, branches and HEAD are as they were

### S4 - A shell metacharacter arrives as a literal argument and is denied
Given a model asks to run `git status ; rm -rf /`, `cat a && b`, `ls > out`, "cat `id`" and `ls | wc`
When each answer streams
Then the daemon parses each into argv without a shell, so the metacharacter is one argument among the others
And each is denied without asking for consent
And no file called `out` is created in the Project

### S5 - An argument that leaves the working directory is denied
Given a model asks to run `cat ../secret.txt` and `cat /etc/hosts` in a Project's directory
When each answer streams
Then each is denied without asking for consent, saying the argument leaves the working directory
And nothing outside the Project is read

### S6 - A working directory that is not a Project or one of its worktrees is refused
Given a model asks to run `ls` in a directory that is not a Project and not one of a Project's worktrees
When the answer streams
Then it is refused without asking for consent, saying the directory is not a Project or one of its worktrees
And the refusal names the Projects Owl knows

### S7 - A Job's worktree is a working directory the chat may use
Given a Job has a worktree on disk
When a model asks to run `git status --short` in that worktree and the user allows it
Then the command runs there, and its output is about the worktree rather than about the Project

### S8 - Consent is asked before each command
Given the user allowed one command once
When the model asks for a second command in the same conversation
Then consent is asked again, naming the second command
And nothing runs until it is answered

### S9 - Remembering for the conversation stops the asking
Given the user answered the first command with "allow, and remember for this conversation"
When the model asks for another command in that conversation
Then no consent is asked for it
And it runs and its output is shown in the chat

### S10 - A grant is for one conversation and not for the next
Given the user remembered a grant in one conversation
When a model asks to run a command in a different conversation
Then consent is asked there

### S11 - Denying a command runs nothing and says so
Given a model asks to run `git log --oneline` and the user denies it
When the answer streams
Then nothing is executed
And the model is told the user refused, and answers without the output

### S12 - A command that fails carries its status and what it printed
Given a model asks to run `cat missing.txt` in a Project's directory and the user allows it
When the command exits non-zero
Then the chat shows what it printed and that it exited non-zero
And the model is given both rather than an empty answer

### S13 - The daemon runs the command itself, with no Agent and no Job
Given a model asks to run `wc -c owl.txt` in a Project's directory and the user allows it
When the command has run
Then its output is the size of the file the Project holds
And no Job and no Run was created, and no Agent was started

### S14 - What the model is given is bounded
Given a Project holds a file larger than one tool result carries
When a model asks to run `cat big.txt` and the user allows it
Then what the model is given is cut to that bound rather than carrying the whole file

### S15 - The app's chat asks for consent and shows what ran
Given the desktop chat
When its sources are read
Then they show the command being proposed with its program, arguments and directory
And offer allowing it once, allowing it for the conversation, and refusing
And render what the command printed and how it exited
And the conversation the daemon keeps holds what was said - one turn each way - rather than what a command printed beyond what the model quoted, so the app's own copy is the only record of it

### S16 - An argument that is a link out of the working directory is denied
Given a Project holds a symbolic link to a file outside it
When a model asks to read that link and the answer streams
Then it is denied without asking for consent, saying the link is outside the working directory
And what is outside the Project does not reach the model

### S17 - What is inside .git is not what is in the repository
Given a Project whose .git holds the credential its remote is reached with
When a model asks to read it - by naming it, by naming it in another case, and by a link that leads there
Then each is denied without asking for consent
And when a model instead greps the whole Project, that runs and finds nothing, because what it would have found is not walked into
And what .git holds does not reach the model, whichever way it was asked for
