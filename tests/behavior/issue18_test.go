package behavior_test

// Behavior tests for issue #18. Each TestS<n> maps to scenario S<n> in
// tests/behavior/issue-18.md. The CLI scenarios drive the built owl binary
// against a running daemon; the chat itself is driven through the desktop
// app's Go side, because the chat has no CLI surface.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vojtechmares/coding-owl/internal/desktop"
)

// fakeProvider is a model provider a scenario runs itself: it records what the
// daemon asked for and answers with the event stream that provider speaks.
type fakeProvider struct {
	server *httptest.Server
	mu     sync.Mutex
	// requests are the bodies the daemon sent, in order.
	requests []map[string]any
	// replies are the responses to send, one per request; the last is reused.
	replies []string
	// breakAfter, when set, writes that much and then hangs up.
	breakAfter string
}

// newFakeProvider starts one, answering every request with those bodies in
// turn. A body is written as the provider's own event stream, so the scenarios
// hold what a provider really sends.
func newFakeProvider(t *testing.T, replies ...string) *fakeProvider {
	t.Helper()
	p := &fakeProvider{replies: replies}
	p.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		p.mu.Lock()
		p.requests = append(p.requests, body)
		n := len(p.requests) - 1
		reply := ""
		switch {
		case len(p.replies) == 0:
		case n < len(p.replies):
			reply = p.replies[n]
		default:
			reply = p.replies[len(p.replies)-1]
		}
		broken := p.breakAfter
		p.mu.Unlock()

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		if broken != "" {
			_, _ = w.Write([]byte(broken))
			if flusher != nil {
				flusher.Flush()
			}
			// Hanging up mid-answer is what a provider that fails looks like.
			if hj, ok := w.(http.Hijacker); ok {
				conn, _, err := hj.Hijack()
				if err == nil {
					_ = conn.Close()
				}
			}
			return
		}
		for _, chunk := range strings.SplitAfter(reply, "\n\n") {
			if chunk == "" {
				continue
			}
			_, _ = w.Write([]byte(chunk))
			if flusher != nil {
				flusher.Flush()
			}
		}
	}))
	t.Cleanup(p.server.Close)
	return p
}

// asked is what the daemon sent the provider, by request.
func (p *fakeProvider) asked(t *testing.T, n int) map[string]any {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		p.mu.Lock()
		if len(p.requests) > n {
			req := p.requests[n]
			p.mu.Unlock()
			return req
		}
		p.mu.Unlock()
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("the provider was not asked %d time(s)", n+1)
	return nil
}

// anthropicText is what the Anthropic event stream looks like for an answer
// made of those pieces.
func anthropicText(pieces ...string) string {
	var b strings.Builder
	b.WriteString("event: message_start\ndata: {\"type\":\"message_start\"}\n\n")
	b.WriteString("event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0," +
		"\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n")
	for _, piece := range pieces {
		delta, _ := json.Marshal(piece)
		fmt.Fprintf(&b, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,"+
			"\"delta\":{\"type\":\"text_delta\",\"text\":%s}}\n\n", delta)
	}
	b.WriteString("event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n")
	b.WriteString("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
	return b.String()
}

// anthropicToolUse is the stream for an answer that asks for a tool.
func anthropicToolUse(id, name string, input map[string]any) string {
	raw, _ := json.Marshal(input)
	partial, _ := json.Marshal(string(raw))
	var b strings.Builder
	b.WriteString("event: message_start\ndata: {\"type\":\"message_start\"}\n\n")
	fmt.Fprintf(&b, "event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,"+
		"\"content_block\":{\"type\":\"tool_use\",\"id\":%q,\"name\":%q,\"input\":{}}}\n\n", id, name)
	fmt.Fprintf(&b, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,"+
		"\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":%s}}\n\n", partial)
	b.WriteString("event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n")
	b.WriteString("event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"tool_use\"}}\n\n")
	b.WriteString("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
	return b.String()
}

// chatLayout is a layout with a daemon and one provider configured against a
// fake, which is what every chat scenario starts from.
func chatLayout(t *testing.T, replies ...string) (*layout, *fakeProvider) {
	t.Helper()
	l := newLayout(t)
	globalConfig(t, l, fileStore)
	daemonUp(t, l)
	p := newFakeProvider(t, replies...)
	addProvider(t, l, "anthropic", "sk-ant-test", p.server.URL)
	return l, p
}

// addProvider configures a model provider through the CLI, which is where a
// key is typed: the app never holds one.
func addProvider(t *testing.T, l *layout, name, key, baseURL string, models ...string) result {
	t.Helper()
	args := []string{"providers", "add", name, "--key-stdin"}
	if baseURL != "" {
		args = append(args, "--base-url", baseURL)
	}
	for _, m := range models {
		args = append(args, "--model", m)
	}
	// The key goes in on standard input: an argument is in the shell history
	// and in what every process on the machine can see (ADR-0019).
	res := runOwlStdin(t, l, key, args...)
	if res.code != 0 {
		t.Fatalf("owl %v exited %d\nstdout:\n%s\nstderr:\n%s", args, res.code, res.stdout, res.stderr)
	}
	return res
}

// chatEvents collects what the app emitted about a conversation.
type chatEvents struct {
	deltas []string
	ended  bool
	err    string
}

// chatting sends a message through the app and collects what it emitted, up to
// the end of the answer.
func chatting(t *testing.T, app *desktop.App, ev *events, conversation int64, model, text string) (int64, chatEvents) {
	t.Helper()
	id, err := app.Send(conversation, model, text)
	if err != nil {
		t.Fatalf("the app could not send a message: %v", err)
	}
	var got chatEvents
	for !got.ended {
		e := ev.next(t)
		switch e.name {
		case desktop.EventChatDelta:
			d, ok := e.data.(desktop.ChatDelta)
			if !ok {
				t.Fatalf("a chat delta carried %T", e.data)
			}
			got.deltas = append(got.deltas, d.Text)
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

func TestS1ChatAProviderIsConfiguredFromTheCLI(t *testing.T) {
	l := newLayout(t)
	globalConfig(t, l, fileStore)
	daemonUp(t, l)
	p := newFakeProvider(t)

	res := addProvider(t, l, "anthropic", "sk-ant-test", p.server.URL)

	if !strings.Contains(res.stdout, "anthropic") {
		t.Errorf("owl providers add does not say what it added:\n%s", res.stdout)
	}
	listed := mustOwl(t, l, "providers", "list").stdout
	if !strings.Contains(listed, "anthropic") {
		t.Errorf("owl providers list does not report it:\n%s", listed)
	}
	if !strings.Contains(listed, "claude") {
		t.Errorf("owl providers list reports no models for anthropic:\n%s", listed)
	}
	// The database is not where a secret goes (ADR-0019).
	if strings.Contains(string(database(t, l)), "sk-ant-test") {
		t.Error("the provider's key is in owl.db")
	}
	// Nor is a key taken as an argument, where the shell and every process on
	// the machine would see it.
	refused := runOwl(t, l, "providers", "add", "openrouter", "--model", "openai/gpt-5")
	if refused.code == 0 {
		t.Fatalf("a provider was configured with no key at all:\n%s", refused.stdout)
	}
	if !strings.Contains(refused.stderr, "--key-stdin") {
		t.Errorf("stderr does not say where a key is read from:\n%s", refused.stderr)
	}
}

func TestS2ChatAnOpenRouterProviderCarriesItsModels(t *testing.T) {
	l := newLayout(t)
	daemonUp(t, l)

	addProvider(t, l, "openrouter", "sk-or-test", "",
		"anthropic/claude-sonnet-4.5", "openai/gpt-5")

	listed := mustOwl(t, l, "providers", "list").stdout
	for _, want := range []string{"openrouter", "anthropic/claude-sonnet-4.5", "openai/gpt-5"} {
		if !strings.Contains(listed, want) {
			t.Errorf("owl providers list does not report %q:\n%s", want, listed)
		}
	}
	res := runOwlStdin(t, l, "sk-or-2", "providers", "add", "openrouter", "--key-stdin")
	if res.code == 0 {
		t.Fatalf("openrouter with no model was accepted:\n%s", res.stdout)
	}
	if !strings.Contains(res.stderr, "--model") {
		t.Errorf("stderr does not name the flag:\n%s", res.stderr)
	}
}

func TestS3ChatProvidersRemoveTakesTheKeyWithIt(t *testing.T) {
	l := newLayout(t)
	globalConfig(t, l, fileStore)
	daemonUp(t, l)
	p := newFakeProvider(t)
	addProvider(t, l, "anthropic", "sk-ant-test", p.server.URL)

	mustOwl(t, l, "providers", "remove", "anthropic")

	if got := mustOwl(t, l, "providers", "list").stdout; strings.Contains(got, "anthropic") {
		t.Errorf("owl providers list still reports it:\n%s", got)
	}
	for ref, secret := range credentials(t, l) {
		if strings.Contains(secret, "sk-ant-test") {
			t.Errorf("the credential store still holds the key, under %s", ref)
		}
	}
	res := runOwl(t, l, "providers", "remove", "anthropic")
	if res.code == 0 {
		t.Fatal("removing a provider that is not there exited 0")
	}
	if !strings.Contains(res.stderr, "anthropic") {
		t.Errorf("stderr does not name it:\n%s", res.stderr)
	}
}

func TestS4ChatTheCLIHasNoChat(t *testing.T) {
	l := newLayout(t)

	help := mustOwl(t, l, "--help").stdout

	// The commands Owl has, which is what the help lists under that heading.
	_, commands, ok := strings.Cut(help, "Available Commands:")
	if !ok {
		t.Fatalf("owl --help lists no commands:\n%s", help)
	}
	commands, _, _ = strings.Cut(commands, "\nFlags:")
	for _, ln := range strings.Split(commands, "\n") {
		if name, _, _ := strings.Cut(strings.TrimSpace(ln), " "); name == "chat" {
			t.Errorf("owl --help offers a chat command:\n%s", commands)
		}
	}
	res := runOwl(t, l, "chat")
	if res.code == 0 {
		t.Fatalf("owl chat exited 0:\n%s", res.stdout)
	}
	if !strings.Contains(res.stderr, "chat") {
		t.Errorf("stderr does not say what was not a command:\n%s", res.stderr)
	}
}

func TestS5ChatTheAppListsTheModelsOfEveryProvider(t *testing.T) {
	l := newLayout(t)
	daemonUp(t, l)
	p := newFakeProvider(t)
	addProvider(t, l, "anthropic", "sk-ant-test", p.server.URL)
	addProvider(t, l, "openrouter", "sk-or-test", "", "openai/gpt-5")
	app, _ := desktopApp(t, l)

	models, err := app.Models()

	if err != nil {
		t.Fatalf("the app could not list models: %v", err)
	}
	byID := map[string]string{}
	for _, m := range models {
		byID[m.ID] = m.Provider
	}
	if byID["openai/gpt-5"] != "openrouter" {
		t.Errorf("models = %+v, want openai/gpt-5 from openrouter", models)
	}
	var anthropic bool
	for id, provider := range byID {
		if provider == "anthropic" && strings.Contains(id, "claude") {
			anthropic = true
		}
	}
	if !anthropic {
		t.Errorf("models = %+v, want anthropic's own models too", models)
	}
}

func TestS6ChatAMessageStreamsBackThroughTheDaemon(t *testing.T) {
	l, p := chatLayout(t, anthropicText("Job 7 ", "was blocked by ", "the test check."))
	app, ev := desktopApp(t, l)

	_, got := chatting(t, app, ev, 0, anthropicModel(t, app), "why was job 7 blocked")

	if len(got.deltas) < 2 {
		t.Errorf("the app emitted %v, want the pieces as they arrived", got.deltas)
	}
	if answer := strings.Join(got.deltas, ""); answer != "Job 7 was blocked by the test check." {
		t.Errorf("the answer assembles to %q", answer)
	}
	if got.err != "" {
		t.Errorf("the answer ended with %q", got.err)
	}
	if _, ok := p.asked(t, 0)["messages"]; !ok {
		t.Errorf("the provider was not sent the conversation: %+v", p.asked(t, 0))
	}
}

func TestS7ChatAConversationIsThereAfterTheAppRestarts(t *testing.T) {
	l, _ := chatLayout(t, anthropicText("It was the tests."))
	app, ev := desktopApp(t, l)
	id, _ := chatting(t, app, ev, 0, anthropicModel(t, app), "why was job 7 blocked")

	again, _ := desktopApp(t, l)
	got, err := again.Conversation(id)

	if err != nil {
		t.Fatalf("the app could not read the conversation back: %v", err)
	}
	var asked, answered bool
	for _, m := range got.Messages {
		if m.Role == "user" && strings.Contains(m.Text, "why was job 7 blocked") {
			asked = true
		}
		if m.Role == "assistant" && strings.Contains(m.Text, "It was the tests.") {
			answered = true
		}
	}
	if !asked || !answered {
		t.Errorf("the conversation carries %+v, want what was said and what came back", got.Messages)
	}
	// What the model said is never the title, so finding it proves the turns
	// themselves are in the database rather than only the conversation.
	if !strings.Contains(string(database(t, l)), "It was the tests.") {
		t.Error("what was said is not in owl.db, so the daemon is keeping it somewhere else")
	}
}

func TestS8ChatTheAppListsConversationsMostRecentFirst(t *testing.T) {
	l, _ := chatLayout(t, anthropicText("first"), anthropicText("second"), anthropicText("third"))
	app, ev := desktopApp(t, l)
	model := anthropicModel(t, app)
	older, _ := chatting(t, app, ev, 0, model, "the older conversation")
	newer, _ := chatting(t, app, ev, 0, model, "the newer conversation")
	// The older one is spoken to last, so it is the one most recently used.
	chatting(t, app, ev, older, model, "and one more thing")

	got, err := app.Conversations()

	if err != nil {
		t.Fatalf("the app could not list conversations: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("the app lists %d conversations, want two: %+v", len(got), got)
	}
	if got[0].ID != older || got[1].ID != newer {
		t.Errorf("conversations are %+v, want the most recently spoken to first", got)
	}
	// Each one, so a title taken from the wrong conversation - or the same
	// title on both - is not mistaken for what they are about.
	if !strings.Contains(got[0].Title, "the older conversation") ||
		!strings.Contains(got[1].Title, "the newer conversation") {
		t.Errorf("the conversations are titled %q and %q, want what each is about",
			got[0].Title, got[1].Title)
	}
}

func TestS9ChatTheToolsOfferedOnlyRead(t *testing.T) {
	l, p := chatLayout(t, anthropicText("nothing to do"))
	app, ev := desktopApp(t, l)

	chatting(t, app, ev, 0, anthropicModel(t, app), "hello")

	tools, _ := p.asked(t, 0)["tools"].([]any)
	names := map[string]bool{}
	for _, tool := range tools {
		if m, ok := tool.(map[string]any); ok {
			names[fmt.Sprint(m["name"])] = true
		}
	}
	for _, want := range []string{"list_projects", "list_jobs", "get_job", "read_run_log", "get_job_diff"} {
		if !names[want] {
			t.Errorf("the model was not offered %s: %v", want, names)
		}
	}
	// Exactly those five: a tool that acts is one more, whatever it is called,
	// and a blocklist of words would not catch `accept_job`.
	if len(names) != 5 {
		t.Errorf("the model was offered %v, want only the five that read", names)
	}
}

func TestS10ChatAQuestionAboutOwlsStateIsAnsweredFromIt(t *testing.T) {
	l, _ := agentLayout(t, agentScript, 0)
	daemonUp(t, l)
	// The check is named something that is not a word of what it prints, so
	// "the check that failed" is checked apart from "what it printed".
	checkedProject(t, l, `apiVersion: codingowl.dev/v1
checks:
  - name: gauntlet
    run: echo the tests are unhappy; exit 1
`)
	out, job := finishedJob(t, l)
	if got := line(t, out, "state"); got != "blocked" {
		t.Fatalf("the job is %q, want blocked so there is something to ask about", got)
	}
	p := newFakeProvider(t,
		anthropicToolUse("call-1", "get_job", map[string]any{"id": id(t, job)}),
		anthropicText("It was the gauntlet check."))
	addProvider(t, l, "anthropic", "sk-ant-test", p.server.URL)
	app, ev := desktopApp(t, l)

	_, got := chatting(t, app, ev, 0, anthropicModel(t, app), "why was job 1 blocked")

	// What the daemon sent back for the tool call is in the second request.
	second := fmt.Sprint(p.asked(t, 1)["messages"])
	// Which check failed, said as the tool result says it, and what it
	// printed: a result that only says something failed is not an answer.
	for _, want := range []string{"gauntlet: failed", "the tests are unhappy"} {
		if !strings.Contains(second, want) {
			t.Errorf("the tool result does not carry %q:\n%s", want, second)
		}
	}
	// The call the result answers goes back with it, or there is nothing for
	// the provider to attach the answer to.
	raw, err := json.Marshal(p.asked(t, 1)["messages"])
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"type":"tool_use"`, `"call-1"`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("the tool result is carried back without %s, so there is no call to attach it to:\n%s",
				want, raw)
		}
	}
	if answer := strings.Join(got.deltas, ""); !strings.Contains(answer, "It was the gauntlet check.") {
		t.Errorf("the answer after the tool call is %q", answer)
	}
}

func TestS11ChatARunsLogIsAnsweredBounded(t *testing.T) {
	// A log long enough that carrying all of it is a choice, and every line of
	// it different, so the end of it is not the beginning.
	script := make([]string, 0, 2000)
	for at := range 2000 {
		script = append(script, fmt.Sprintf(`{"type":"assistant","message":"line %04d of what it printed"}`, at))
	}
	l, _ := agentLayout(t, script, 0)
	daemonUp(t, l)
	checkedProject(t, l, "apiVersion: codingowl.dev/v1\n")
	out, _ := finishedJob(t, l)
	runID := runRows(t, out)[0].id
	p := newFakeProvider(t,
		anthropicToolUse("call-1", "read_run_log", map[string]any{"run_id": id(t, runID)}),
		anthropicText("It ran."))
	addProvider(t, l, "anthropic", "sk-ant-test", p.server.URL)
	app, ev := desktopApp(t, l)

	chatting(t, app, ev, 0, anthropicModel(t, app), "what did run 1 print")

	second := fmt.Sprint(p.asked(t, 1)["messages"])
	if !strings.Contains(second, "line 1999 of what it printed") {
		t.Errorf("the tool result does not carry the end of the run's log:\n%s", tail(second))
	}
	if strings.Contains(second, "line 0000 of what it printed") {
		t.Errorf("the tool result carries the whole log rather than the end of it (%d bytes)", len(second))
	}
	// What the daemon carries of a log is bounded at 16 KiB, and the request
	// is that plus the shape around it: a bound above what an unbounded log
	// would come to is not a bound.
	if len(second) > 20<<10 {
		t.Errorf("the tool result is %d bytes, want no more than the daemon will carry", len(second))
	}
}

// tail is the end of something long, for a message a person reads.
func tail(text string) string {
	if len(text) <= 500 {
		return text
	}
	return "..." + text[len(text)-500:]
}

func TestS12ChatWhatAJobChangedIsAnsweredWithTheDiff(t *testing.T) {
	l, _ := writingLayout(t, map[string]string{"work.txt": "a line the agent added\n"}, true, agentScript)
	daemonUp(t, l)
	checkedProject(t, l, "apiVersion: codingowl.dev/v1\n")
	_, job := finishedJob(t, l)
	p := newFakeProvider(t,
		anthropicToolUse("call-1", "get_job_diff", map[string]any{"id": id(t, job)}),
		anthropicText("It added a file."))
	addProvider(t, l, "anthropic", "sk-ant-test", p.server.URL)
	app, ev := desktopApp(t, l)

	chatting(t, app, ev, 0, anthropicModel(t, app), "what did job 1 change")

	second := fmt.Sprint(p.asked(t, 1)["messages"])
	for _, want := range []string{"work.txt", "a line the agent added"} {
		if !strings.Contains(second, want) {
			t.Errorf("the tool result does not carry %q:\n%s", want, second)
		}
	}
}

func TestS13ChatAToolTheDaemonDoesNotHaveIsRefused(t *testing.T) {
	l, p := chatLayout(t,
		anthropicToolUse("call-1", "run_command", map[string]any{"argv": []string{"rm", "-rf", "/"}}),
		anthropicText("I cannot do that."))
	r := newRepo(t, l, "api")
	addProject(t, l, r)
	addJob(t, l, r.dir, "work", "--no-plan")
	before := mustOwl(t, l, "status").stdout
	app, ev := desktopApp(t, l)

	_, got := chatting(t, app, ev, 0, anthropicModel(t, app), "delete everything")

	second := fmt.Sprint(p.asked(t, 1)["messages"])
	if !strings.Contains(second, "run_command") || !strings.Contains(second, "not a tool") {
		t.Errorf("the model was not told the tool is not one Owl has:\n%s", second)
	}
	if got.err != "" {
		t.Errorf("the stream failed with %q, want the model told instead", got.err)
	}
	if answer := strings.Join(got.deltas, ""); !strings.Contains(answer, "I cannot do that.") {
		t.Errorf("the answer after the refusal is %q", answer)
	}
	// Nothing about Owl's state changed: the same Projects, the same queue.
	if after := mustOwl(t, l, "status").stdout; after != before {
		t.Errorf("what Owl holds changed while the model was asking for a tool:\nbefore:\n%s\nafter:\n%s",
			before, after)
	}
	if got := mustOwl(t, l, "project", "list").stdout; !strings.Contains(got, "api") {
		t.Errorf("the project is gone:\n%s", got)
	}
}

func TestS14ChatAProviderThatFailsMidAnswerEndsTheStream(t *testing.T) {
	l := newLayout(t)
	daemonUp(t, l)
	p := newFakeProvider(t)
	p.breakAfter = "event: message_start\ndata: {\"type\":\"message_start\"}\n\n" +
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0," +
		"\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0," +
		"\"delta\":{\"type\":\"text_delta\",\"text\":\"half an ans\"}}\n\n"
	addProvider(t, l, "anthropic", "sk-ant-test", p.server.URL)
	app, ev := desktopApp(t, l)

	id, got := chatting(t, app, ev, 0, anthropicModel(t, app), "why was job 7 blocked")

	if !strings.Contains(got.err, "anthropic") {
		t.Errorf("the answer ended with %q, want an error naming the provider", got.err)
	}
	if strings.Join(got.deltas, "") != "half an ans" {
		t.Errorf("the app was given %v, want what arrived before the break", got.deltas)
	}
	kept, err := app.Conversation(id)
	if err != nil {
		t.Fatalf("the conversation could not be read back: %v", err)
	}
	var found bool
	for _, m := range kept.Messages {
		if m.Role == "assistant" && strings.Contains(m.Text, "half an ans") {
			found = true
		}
	}
	if !found {
		t.Errorf("what arrived was not kept: %+v", kept.Messages)
	}
}

func TestS15ChatTheAppNeverHoldsAProviderKey(t *testing.T) {
	l, _ := chatLayout(t, anthropicText("hello"))
	app, ev := desktopApp(t, l)
	id, _ := chatting(t, app, ev, 0, anthropicModel(t, app), "hello")

	models, err := app.Models()
	if err != nil {
		t.Fatalf("Models: %v", err)
	}
	conversations, err := app.Conversations()
	if err != nil {
		t.Fatalf("Conversations: %v", err)
	}
	one, err := app.Conversation(id)
	if err != nil {
		t.Fatalf("Conversation: %v", err)
	}

	for what, v := range map[string]any{"models": models, "conversations": conversations, "conversation": one} {
		data, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "sk-ant-test") {
			t.Errorf("what the app was given for %s carries the key:\n%s", what, data)
		}
	}
	_, err = app.Send(0, "openrouter/nothing", "hello")
	if err == nil {
		t.Fatal("the app sent a message for a provider that is not configured")
	}
	if !strings.Contains(err.Error(), "openrouter/nothing") {
		t.Errorf("the refusal is %q, want it to say what was asked for", err)
	}
}

// anthropicModel is a model the configured anthropic provider offers.
func anthropicModel(t *testing.T, app *desktop.App) string {
	t.Helper()
	models, err := app.Models()
	if err != nil {
		t.Fatalf("the app could not list models: %v", err)
	}
	for _, m := range models {
		if m.Provider == "anthropic" {
			return m.ID
		}
	}
	t.Fatalf("no anthropic model is configured: %+v", models)
	return ""
}

func TestS16ChatTheAppsWindowOffersTheChat(t *testing.T) {
	// The app cannot be driven without a display, so what the frontend is made
	// of is checked on disk, as issue #9 checks its theme.
	src := filepath.Join(repoDir, "cmd", "owl-desktop", "frontend", "src")
	for path, wants := range map[string][]string{
		filepath.Join("views", "Chat.tsx"): {"api.sendTo(", "api.models(", "api.conversations(", "EVENT_CHAT_DELTA"},
		filepath.Join("lib", "api.ts"):     {"App.SendTo(", "App.Models(", "App.Conversations(", "App.Conversation("},
		"App.tsx":                          {"views/Chat", "<Chat", "label: \"Chat\""},
	} {
		body := readFile(t, filepath.Join(src, path))
		if body == "" {
			t.Errorf("%s is not there", path)
			continue
		}
		for _, want := range wants {
			if !strings.Contains(body, want) {
				t.Errorf("%s does not carry %q", path, want)
			}
		}
	}
	// The provider goes to the daemon with the model, rather than the picker
	// naming it only for itself: two providers may offer the same model.
	sending := regexp.MustCompile(`api\.sendTo\([^)]*chosen\.Provider`)
	if body := readFile(t, filepath.Join(src, "views", "Chat.tsx")); !sending.MatchString(body) {
		t.Error("the chat view does not send naming the provider the model comes from")
	}
}
