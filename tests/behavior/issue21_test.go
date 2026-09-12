package behavior_test

// Behavior tests for issue #21. Each TestS<n> maps to scenario S<n> in
// tests/behavior/issue-21.md. The chat has no CLI surface, so the scenarios
// drive the desktop app's Go side against a daemon and a fake provider, the
// way the issue #18 scenarios do.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vojtechmares/coding-owl/internal/desktop"
)

// commandLayout is a chat layout with one Project registered, which is what
// every command scenario needs: a directory a command may run in.
func commandLayout(t *testing.T, replies ...string) (*layout, *repo, *fakeProvider) {
	t.Helper()
	l, p := chatLayout(t, replies...)
	r := newRepo(t, l, "api")
	addProject(t, l, r)
	// Something for `git status` to have an opinion about, so a scenario can
	// tell a command that ran from one that did not.
	write(t, r.dir, "owl.txt", "owl was here\n")
	return l, r, p
}

// write puts a file in a directory, failing the test if it cannot.
func write(t *testing.T, dir, name, body string) string {
	t.Helper()
	at := filepath.Join(dir, name)
	if err := os.WriteFile(at, []byte(body), 0o600); err != nil {
		t.Fatalf("writing %s: %v", at, err)
	}
	return at
}

// asksToRun is a provider reply in which the model asks to run one command.
func asksToRun(id, command, directory string) string {
	return anthropicToolUse(id, "run_command", map[string]any{
		"command": command, "directory": directory,
	})
}

// chatCommands is what the app emitted about the commands in one answer.
type chatCommands struct {
	deltas   []string
	proposed []desktop.ChatCommand
	ran      []desktop.ChatCommandDone
	ended    bool
	err      string
}

// answer is what the model was told, which is the second request's messages:
// the first request is the question, and the tool results go back in the next.
func (p *fakeProvider) told(t *testing.T, n int) string {
	t.Helper()
	return fmt.Sprint(p.asked(t, n)["messages"])
}

// commanding sends a message and answers every command the chat proposes with
// decide, collecting what the app emitted up to the end of the answer. A nil
// decide refuses everything, which is what a scenario expecting no proposal
// wants: a proposal that arrives anyway still ends the exchange.
func commanding(t *testing.T, app *desktop.App, ev *events, conversation int64,
	model, text string, decide func(desktop.ChatCommand) string,
) (int64, chatCommands) {
	t.Helper()
	id, err := app.Send(conversation, model, text)
	if err != nil {
		t.Fatalf("the app could not send a message: %v", err)
	}
	var got chatCommands
	for !got.ended {
		e := ev.next(t)
		switch e.name {
		case desktop.EventChatDelta:
			d, ok := e.data.(desktop.ChatDelta)
			if !ok {
				t.Fatalf("a chat delta carried %T", e.data)
			}
			got.deltas = append(got.deltas, d.Text)
		case desktop.EventChatCommand:
			c, ok := e.data.(desktop.ChatCommand)
			if !ok {
				t.Fatalf("a chat command carried %T", e.data)
			}
			got.proposed = append(got.proposed, c)
			decision := desktop.Refuse
			if decide != nil {
				decision = decide(c)
			}
			if err := app.AnswerCommand(c.RequestID, decision); err != nil {
				t.Fatalf("the app could not answer command %q: %v", c.RequestID, err)
			}
		case desktop.EventChatCommandDone:
			done, ok := e.data.(desktop.ChatCommandDone)
			if !ok {
				t.Fatalf("a finished chat command carried %T", e.data)
			}
			got.ran = append(got.ran, done)
		case desktop.EventChatEnd:
			end, ok := e.data.(desktop.ChatEnd)
			if !ok {
				t.Fatalf("a chat end carried %T", e.data)
			}
			got.ended, got.err = true, end.Error
		}
	}
	return id, got
}

// equalArgv reports whether two argv are the same words in the same order.
func equalArgv(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for at := range got {
		if got[at] != want[at] {
			return false
		}
	}
	return true
}

// allowing answers every command the same way.
func allowing(decision string) func(desktop.ChatCommand) string {
	return func(desktop.ChatCommand) string { return decision }
}

func TestS1CommandsAnAllowedCommandRunsWhereItWasAskedFor(t *testing.T) {
	l, r, p := commandLayout(t,
		asksToRun("call-1", "git status --short", ""),
		anthropicText("There is an untracked owl.txt."))
	// The directory is the Project's own, which the model learns from the
	// tools it already has.
	p.replies[0] = asksToRun("call-1", "git status --short", r.dir)
	app, ev := desktopApp(t, l)

	_, got := commanding(t, app, ev, 0, anthropicModel(t, app), "what is uncommitted",
		allowing(desktop.AllowOnce))

	if len(got.proposed) != 1 {
		t.Fatalf("the chat proposed %d commands, want the one the model asked for: %+v", len(got.proposed), got.proposed)
	}
	// What will run is named before it runs: the program, its arguments and
	// where it will run.
	proposal := got.proposed[0]
	if want := []string{"git", "status", "--short"}; !equalArgv(proposal.Argv, want) {
		t.Errorf("the proposal is %v, want %v", proposal.Argv, want)
	}
	if !samePath(proposal.Directory, r.dir) {
		t.Errorf("the proposal runs in %q, want the project's directory %q", proposal.Directory, r.dir)
	}
	if len(got.ran) != 1 {
		t.Fatalf("the chat reported %d commands as having run, want one: %+v", len(got.ran), got.ran)
	}
	if !strings.Contains(got.ran[0].Output, "owl.txt") {
		t.Errorf("what the command printed is %q, want the untracked file in it", got.ran[0].Output)
	}
	if got.ran[0].ExitCode != 0 {
		t.Errorf("the command exited %d, want 0", got.ran[0].ExitCode)
	}
	// And the model is given the same output rather than being left to guess.
	if told := p.told(t, 1); !strings.Contains(told, "owl.txt") {
		t.Errorf("the model was not given what the command printed:\n%s", told)
	}
	if answer := strings.Join(got.deltas, ""); !strings.Contains(answer, "untracked owl.txt") {
		t.Errorf("the answer after the command is %q", answer)
	}
}

func TestS2CommandsAProgramThatIsNotAllowedIsDenied(t *testing.T) {
	l, r, p := commandLayout(t, "", anthropicText("I cannot run that."))
	p.replies[0] = asksToRun("call-1", "rm -rf .", r.dir)
	app, ev := desktopApp(t, l)

	_, got := commanding(t, app, ev, 0, anthropicModel(t, app), "clean it up",
		allowing(desktop.AllowOnce))

	if len(got.proposed) != 0 {
		t.Errorf("consent was asked for a command that is denied outright: %+v", got.proposed)
	}
	if len(got.ran) != 0 {
		t.Errorf("a command that is denied outright ran anyway: %+v", got.ran)
	}
	told := p.told(t, 1)
	if !strings.Contains(told, "rm") {
		t.Errorf("the model was not told which program was refused:\n%s", told)
	}
	// And what it may run instead, or being told no is not an answer it can
	// use.
	if !strings.Contains(told, "git") || !strings.Contains(told, "grep") {
		t.Errorf("the refusal does not say what owl does run:\n%s", told)
	}
	// The file the Project holds is still there.
	if _, err := os.Stat(filepath.Join(r.dir, "owl.txt")); err != nil {
		t.Errorf("the project's file is gone: %v", err)
	}
}

func TestS3CommandsTheGitSubcommandsThatWriteAreDenied(t *testing.T) {
	for _, command := range []string{
		"git checkout main", "git reset --hard", "git clean -fd", "git push",
	} {
		t.Run(command, func(t *testing.T) {
			l, r, p := commandLayout(t, "", anthropicText("I cannot run that."))
			p.replies[0] = asksToRun("call-1", command, r.dir)
			app, ev := desktopApp(t, l)
			before := gitIn(t, r, r.dir, "rev-parse", "HEAD")

			_, got := commanding(t, app, ev, 0, anthropicModel(t, app), "do it",
				allowing(desktop.AllowOnce))

			if len(got.proposed) != 0 {
				t.Errorf("consent was asked for %q: %+v", command, got.proposed)
			}
			if len(got.ran) != 0 {
				t.Errorf("%q ran anyway: %+v", command, got.ran)
			}
			sub := strings.Fields(command)[1]
			if told := p.told(t, 1); !strings.Contains(told, sub) {
				t.Errorf("the model was not told that git %s is refused:\n%s", sub, told)
			}
			// And the repository is where it was.
			if after := gitIn(t, r, r.dir, "rev-parse", "HEAD"); after != before {
				t.Errorf("HEAD moved from %s to %s", before, after)
			}
			if _, err := os.Stat(filepath.Join(r.dir, "owl.txt")); err != nil {
				t.Errorf("the untracked file is gone: %v", err)
			}
		})
	}
}

func TestS4CommandsAShellMetacharacterIsALiteralArgumentAndIsDenied(t *testing.T) {
	for _, command := range []string{
		"git status ; rm -rf /",
		"cat a && b",
		"ls > out",
		"cat `id`",
		"ls | wc",
	} {
		t.Run(command, func(t *testing.T) {
			l, r, p := commandLayout(t, "", anthropicText("I cannot run that."))
			p.replies[0] = asksToRun("call-1", command, r.dir)
			app, ev := desktopApp(t, l)

			_, got := commanding(t, app, ev, 0, anthropicModel(t, app), "do it",
				allowing(desktop.AllowOnce))

			if len(got.proposed) != 0 {
				t.Errorf("consent was asked for %q: %+v", command, got.proposed)
			}
			if len(got.ran) != 0 {
				t.Errorf("%q ran anyway: %+v", command, got.ran)
			}
			// Nothing interpreted the metacharacter, so nothing redirected.
			if _, err := os.Stat(filepath.Join(r.dir, "out")); err == nil {
				t.Errorf("%q created a file called out, so something ran a shell", command)
			}
		})
	}
}

func TestS5CommandsAnArgumentThatLeavesTheWorkingDirectoryIsDenied(t *testing.T) {
	for _, command := range []string{"cat ../secret.txt", "cat /etc/hosts"} {
		t.Run(command, func(t *testing.T) {
			l, r, p := commandLayout(t, "", anthropicText("I cannot read that."))
			write(t, filepath.Dir(r.dir), "secret.txt", "the password is hunter2\n")
			p.replies[0] = asksToRun("call-1", command, r.dir)
			app, ev := desktopApp(t, l)

			_, got := commanding(t, app, ev, 0, anthropicModel(t, app), "read it",
				allowing(desktop.AllowOnce))

			if len(got.proposed) != 0 {
				t.Errorf("consent was asked for %q: %+v", command, got.proposed)
			}
			if len(got.ran) != 0 {
				t.Errorf("%q ran anyway: %+v", command, got.ran)
			}
			told := p.told(t, 1)
			if !strings.Contains(told, "working directory") {
				t.Errorf("the model was not told the argument leaves the working directory:\n%s", told)
			}
			// And nothing outside the Project was read into the conversation.
			if strings.Contains(told, "hunter2") {
				t.Errorf("what is outside the project reached the model:\n%s", told)
			}
		})
	}
}

func TestS6CommandsADirectoryThatIsNoProjectIsRefused(t *testing.T) {
	l, _, p := commandLayout(t, "", anthropicText("I cannot work there."))
	elsewhere := t.TempDir()
	p.replies[0] = asksToRun("call-1", "ls", elsewhere)
	app, ev := desktopApp(t, l)

	_, got := commanding(t, app, ev, 0, anthropicModel(t, app), "what is in there",
		allowing(desktop.AllowOnce))

	if len(got.proposed) != 0 {
		t.Errorf("consent was asked for a directory that is not a project: %+v", got.proposed)
	}
	if len(got.ran) != 0 {
		t.Errorf("a command ran outside every project: %+v", got.ran)
	}
	told := p.told(t, 1)
	for _, want := range []string{"Project", elsewhere} {
		if !strings.Contains(told, want) {
			t.Errorf("the refusal does not carry %q:\n%s", want, told)
		}
	}
	// And the model is told where it may work instead.
	if !strings.Contains(told, "api") {
		t.Errorf("the refusal does not name the projects owl knows:\n%s", told)
	}
}

func TestS7CommandsAJobsWorktreeIsADirectoryTheChatMayUse(t *testing.T) {
	l, _ := agentLayout(t, agentScript, 0)
	daemonUp(t, l)
	r := checkedProject(t, l, "apiVersion: codingowl.dev/v1\n")
	out, job := finishedJob(t, l)
	worktree := line(t, out, "worktree")
	if worktree == "" || worktree == "(none)" {
		t.Fatalf("job %s has no worktree to work in:\n%s", job, out)
	}
	write(t, worktree, "only-here.txt", "this file is not in the project\n")
	p := newFakeProvider(t,
		asksToRun("call-1", "git status --short", worktree),
		anthropicText("Only the worktree has it."))
	addProvider(t, l, "anthropic", "sk-ant-test", p.server.URL)
	app, ev := desktopApp(t, l)

	_, got := commanding(t, app, ev, 0, anthropicModel(t, app), "what is in the worktree",
		allowing(desktop.AllowOnce))

	if len(got.ran) != 1 {
		t.Fatalf("the chat reported %d commands as having run, want one: %+v", len(got.ran), got.ran)
	}
	if !samePath(got.ran[0].Directory, worktree) {
		t.Errorf("the command ran in %q, want the job's worktree %q", got.ran[0].Directory, worktree)
	}
	if !strings.Contains(got.ran[0].Output, "only-here.txt") {
		t.Errorf("what it printed is %q, want the file only the worktree has", got.ran[0].Output)
	}
	// And it is the worktree rather than the Project: the file is not there.
	if _, err := os.Stat(filepath.Join(r.dir, "only-here.txt")); err == nil {
		t.Fatal("the file the scenario put in the worktree is in the project too, so this proves nothing")
	}
}

func TestS8CommandsConsentIsAskedBeforeEachCommand(t *testing.T) {
	l, r, p := commandLayout(t, "", "", anthropicText("Both are done."))
	p.replies[0] = asksToRun("call-1", "git status --short", r.dir)
	p.replies[1] = asksToRun("call-2", "wc -c owl.txt", r.dir)
	app, ev := desktopApp(t, l)

	_, got := commanding(t, app, ev, 0, anthropicModel(t, app), "look twice",
		allowing(desktop.AllowOnce))

	if len(got.proposed) != 2 {
		t.Fatalf("the chat asked %d times, want once for each command: %+v", len(got.proposed), got.proposed)
	}
	// The second asking is about the second command, not a repeat of the
	// first.
	if want := []string{"wc", "-c", "owl.txt"}; !equalArgv(got.proposed[1].Argv, want) {
		t.Errorf("the second proposal is %v, want %v", got.proposed[1].Argv, want)
	}
	if got.proposed[0].RequestID == got.proposed[1].RequestID {
		t.Errorf("both proposals carry the same id %q, so one answer would answer both", got.proposed[0].RequestID)
	}
	if len(got.ran) != 2 {
		t.Errorf("the chat reported %d commands as having run, want two: %+v", len(got.ran), got.ran)
	}
}

func TestS9CommandsRememberingForTheConversationStopsTheAsking(t *testing.T) {
	l, r, p := commandLayout(t, "", "", anthropicText("Both are done."))
	p.replies[0] = asksToRun("call-1", "git status --short", r.dir)
	p.replies[1] = asksToRun("call-2", "wc -c owl.txt", r.dir)
	app, ev := desktopApp(t, l)

	_, got := commanding(t, app, ev, 0, anthropicModel(t, app), "look twice",
		allowing(desktop.AllowConversation))

	if len(got.proposed) != 1 {
		t.Fatalf("the chat asked %d times, want only the first: %+v", len(got.proposed), got.proposed)
	}
	if len(got.ran) != 2 {
		t.Fatalf("the chat reported %d commands as having run, want both: %+v", len(got.ran), got.ran)
	}
	if !strings.Contains(got.ran[1].Output, "owl was here") && !strings.Contains(got.ran[1].Output, "13") {
		t.Errorf("the second command printed %q, want the size of the file", got.ran[1].Output)
	}
}

func TestS10CommandsAGrantIsForOneConversationOnly(t *testing.T) {
	l, r, p := commandLayout(t, "", anthropicText("Done."), "", anthropicText("Done again."))
	p.replies[0] = asksToRun("call-1", "git status --short", r.dir)
	p.replies[2] = asksToRun("call-2", "git status --short", r.dir)
	app, ev := desktopApp(t, l)

	first, got := commanding(t, app, ev, 0, anthropicModel(t, app), "look here",
		allowing(desktop.AllowConversation))
	if len(got.proposed) != 1 {
		t.Fatalf("the first conversation asked %d times, want once: %+v", len(got.proposed), got.proposed)
	}

	// A different conversation, which the grant does not reach.
	_, again := commanding(t, app, ev, 0, anthropicModel(t, app), "look there",
		allowing(desktop.AllowOnce))

	if len(again.proposed) != 1 {
		t.Errorf("the second conversation asked %d times, want once: %+v", len(again.proposed), again.proposed)
	}
	if first == 0 {
		t.Error("the first conversation has no id")
	}
}

func TestS11CommandsRefusingRunsNothingAndSaysSo(t *testing.T) {
	l, r, p := commandLayout(t, "", anthropicText("You did not want me to."))
	p.replies[0] = asksToRun("call-1", "git log --oneline", r.dir)
	app, ev := desktopApp(t, l)

	_, got := commanding(t, app, ev, 0, anthropicModel(t, app), "what is the history",
		allowing(desktop.Refuse))

	if len(got.proposed) != 1 {
		t.Fatalf("the chat asked %d times, want once: %+v", len(got.proposed), got.proposed)
	}
	if len(got.ran) != 0 {
		t.Errorf("a command the user refused ran anyway: %+v", got.ran)
	}
	told := p.told(t, 1)
	if !strings.Contains(told, "refused") {
		t.Errorf("the model was not told the user refused:\n%s", told)
	}
	if answer := strings.Join(got.deltas, ""); !strings.Contains(answer, "You did not want me to.") {
		t.Errorf("the answer after the refusal is %q", answer)
	}
}

func TestS12CommandsACommandThatFailsCarriesItsStatusAndOutput(t *testing.T) {
	l, r, p := commandLayout(t, "", anthropicText("There is no such file."))
	p.replies[0] = asksToRun("call-1", "cat missing.txt", r.dir)
	app, ev := desktopApp(t, l)

	_, got := commanding(t, app, ev, 0, anthropicModel(t, app), "read missing.txt",
		allowing(desktop.AllowOnce))

	if len(got.ran) != 1 {
		t.Fatalf("the chat reported %d commands as having run, want one: %+v", len(got.ran), got.ran)
	}
	if got.ran[0].ExitCode == 0 {
		t.Errorf("the command exited %d, want a failure", got.ran[0].ExitCode)
	}
	if !strings.Contains(got.ran[0].Output, "missing.txt") {
		t.Errorf("what it printed is %q, want the file it could not read", got.ran[0].Output)
	}
	told := p.told(t, 1)
	if !strings.Contains(told, "missing.txt") {
		t.Errorf("the model was not given what the command printed:\n%s", told)
	}
	// Which is only half of it: the model has to know that it failed.
	if !strings.Contains(told, "exit") {
		t.Errorf("the model was not told how the command exited:\n%s", told)
	}
}

func TestS13CommandsTheDaemonRunsItWithNoAgentAndNoJob(t *testing.T) {
	l, r, p := commandLayout(t, "", anthropicText("Thirteen bytes."))
	p.replies[0] = asksToRun("call-1", "wc -c owl.txt", r.dir)
	app, ev := desktopApp(t, l)

	_, got := commanding(t, app, ev, 0, anthropicModel(t, app), "how big is owl.txt",
		allowing(desktop.AllowOnce))

	if len(got.ran) != 1 {
		t.Fatalf("the chat reported %d commands as having run, want one: %+v", len(got.ran), got.ran)
	}
	// The file is "owl was here\n", which is thirteen bytes: the daemon really
	// read the file the Project holds.
	if !strings.Contains(got.ran[0].Output, "13") {
		t.Errorf("what it printed is %q, want the size of the file", got.ran[0].Output)
	}
	// And nothing was queued to do it: a command is not a Job, and no Agent
	// was asked to run anything (ADR-0022).
	queued := mustOwl(t, l, "queue", "list").stdout
	if !strings.Contains(queued, "queue is empty") {
		t.Errorf("running a command left jobs behind:\n%s", queued)
	}
}

func TestS14CommandsWhatTheModelIsGivenIsBounded(t *testing.T) {
	l, r, p := commandLayout(t, "", anthropicText("It is long."))
	// Longer than one tool result carries, and every line different so the end
	// of it is not the beginning.
	var big strings.Builder
	for at := range 4000 {
		fmt.Fprintf(&big, "line %04d of a file that is longer than owl will carry\n", at)
	}
	write(t, r.dir, "big.txt", big.String())
	p.replies[0] = asksToRun("call-1", "cat big.txt", r.dir)
	app, ev := desktopApp(t, l)

	_, got := commanding(t, app, ev, 0, anthropicModel(t, app), "read big.txt",
		allowing(desktop.AllowOnce))

	if len(got.ran) != 1 {
		t.Fatalf("the chat reported %d commands as having run, want one: %+v", len(got.ran), got.ran)
	}
	told := p.told(t, 1)
	if strings.Contains(told, "line 3999 of a file") {
		t.Errorf("the model was given the whole file (%d bytes)", len(told))
	}
	if len(told) > 20<<10 {
		t.Errorf("the model was given %d bytes, want no more than owl carries", len(told))
	}
}

func TestS15CommandsTheAppAsksForConsentAndShowsWhatRan(t *testing.T) {
	source := readFile(t, filepath.Join(repoDir, "cmd", "owl-desktop", "frontend", "src", "views", "Chat.tsx"))

	// What will run is named before the user answers.
	for _, want := range []string{"c.argv.join", "c.directory"} {
		if !strings.Contains(source, want) {
			t.Errorf("the chat does not show %s before the command runs", want)
		}
	}
	// The three answers a person may give, each reaching the daemon.
	if !strings.Contains(source, "api.answerCommand(") {
		t.Error("the chat never answers a command")
	}
	for _, want := range []string{`decision: "once"`, `decision: "conversation"`, `decision: "refuse"`} {
		if !strings.Contains(source, want) {
			t.Errorf("the chat does not offer %s as an answer", want)
		}
	}
	// And what came of it.
	for _, want := range []string{"done.output", "done.exitCode"} {
		if !strings.Contains(source, want) {
			t.Errorf("the chat does not show %s", want)
		}
	}
}
