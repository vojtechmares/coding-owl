package chat_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/vojtechmares/coding-owl/internal/chat"
	"github.com/vojtechmares/coding-owl/internal/credential"
	"github.com/vojtechmares/coding-owl/internal/store"
)

var ctx = context.Background()

// fixture is a chat over a temporary database and credential store, with a
// view of what Owl knows that the test decides.
type fixture struct {
	chat  *chat.Service
	store *store.Store
	creds credential.Store
	view  *fakeView
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	root := t.TempDir()
	st, _, err := store.Open(filepath.Join(root, "owl.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	creds := credential.NewFile(filepath.Join(root, "credentials.json"))
	view := &fakeView{}
	return &fixture{chat: chat.NewService(st, creds, chat.NewTools(view)), store: st, creds: creds, view: view}
}

// fakeView answers what the tools ask of Owl's state.
type fakeView struct {
	projects string
	jobs     string
	job      string
	log      string
	diff     string
	asked    []string
	err      error
}

func (v *fakeView) Projects(context.Context) (string, error) {
	v.asked = append(v.asked, "projects")
	return v.projects, v.err
}

func (v *fakeView) Jobs(_ context.Context, all bool) (string, error) {
	v.asked = append(v.asked, "jobs")
	_ = all
	return v.jobs, v.err
}

func (v *fakeView) Job(_ context.Context, id int64) (string, error) {
	v.asked = append(v.asked, "job")
	_ = id
	return v.job, v.err
}

func (v *fakeView) RunLog(_ context.Context, id int64, max int) (string, error) {
	v.asked = append(v.asked, "log")
	_, _ = id, max
	return v.log, v.err
}

func (v *fakeView) JobDiff(_ context.Context, id int64, max int) (string, error) {
	v.asked = append(v.asked, "diff")
	_, _ = id, max
	return v.diff, v.err
}

func TestAddProviderKeepsTheKeyOutOfTheDatabase(t *testing.T) {
	f := newFixture(t)

	got, err := f.chat.AddProvider(ctx, "anthropic", "sk-ant-secret", "", nil)

	if err != nil {
		t.Fatalf("AddProvider: %v", err)
	}
	if len(got.Models) == 0 {
		t.Error("a provider Owl knows the models of was configured with none")
	}
	row, err := f.store.GetChatProvider(ctx, "anthropic")
	if err != nil {
		t.Fatalf("GetChatProvider: %v", err)
	}
	if strings.Contains(row.CredentialRef, "sk-ant-secret") {
		t.Errorf("the row carries the key itself: %+v", row)
	}
	secret, err := f.creds.Get(ctx, row.CredentialRef)
	if err != nil || secret != "sk-ant-secret" {
		t.Errorf("the credential store holds %q, %v; want the key", secret, err)
	}
}

func TestAddProviderRefusesWhatOwlCannotUse(t *testing.T) {
	f := newFixture(t)
	for what, add := range map[string]func() error{
		"a provider Owl does not drive": func() error {
			_, err := f.chat.AddProvider(ctx, "some-llm", "k", "", []string{"m"})
			return err
		},
		"no key at all": func() error {
			_, err := f.chat.AddProvider(ctx, "anthropic", "  ", "", nil)
			return err
		},
		"openrouter with no model": func() error {
			_, err := f.chat.AddProvider(ctx, "openrouter", "k", "", nil)
			return err
		},
		"a base url that is not one": func() error {
			_, err := f.chat.AddProvider(ctx, "anthropic", "k", "ftp://models", nil)
			return err
		},
		"a base url carrying a credential": func() error {
			_, err := f.chat.AddProvider(ctx, "anthropic", "k", "https://someone:s3cr3t@models.test", nil)
			return err
		},
		"a model name Owl cannot use": func() error {
			_, err := f.chat.AddProvider(ctx, "openrouter", "k", "", []string{"--upload-pack=evil"})
			return err
		},
	} {
		err := add()

		var invalid *chat.InvalidError
		if !errors.As(err, &invalid) {
			t.Errorf("%s = %v, want it refused", what, err)
		}
	}
	if all, err := f.chat.ListProviders(ctx); err != nil || len(all) != 0 {
		t.Errorf("ListProviders = %+v, %v; want nothing configured", all, err)
	}
}

func TestRemoveProviderTakesTheKeyWithIt(t *testing.T) {
	f := newFixture(t)
	if _, err := f.chat.AddProvider(ctx, "anthropic", "sk-ant-secret", "", nil); err != nil {
		t.Fatalf("AddProvider: %v", err)
	}

	if err := f.chat.RemoveProvider(ctx, "anthropic"); err != nil {
		t.Fatalf("RemoveProvider: %v", err)
	}

	if _, err := f.creds.Get(ctx, "chat/anthropic"); !errors.Is(err, credential.ErrNotFound) {
		t.Errorf("the key is still in the credential store: %v", err)
	}
	var invalid *chat.InvalidError
	if err := f.chat.RemoveProvider(ctx, "anthropic"); !errors.As(err, &invalid) {
		t.Errorf("removing one that is not there = %v, want it refused", err)
	}
}

func TestSendRefusesAModelNobodyConfigured(t *testing.T) {
	f := newFixture(t)

	_, err := f.chat.Send(ctx, chat.SendRequest{Model: "claude-opus-5", Text: "hello"}, nil)

	var invalid *chat.InvalidError
	if !errors.As(err, &invalid) {
		t.Fatalf("Send with no provider = %v, want it refused", err)
	}
	if !strings.Contains(err.Error(), "claude-opus-5") {
		t.Errorf("the error %q does not name the model", err)
	}
	// Nothing was started: a conversation is what a message makes, and there
	// was no message.
	if all, err := f.chat.Conversations(ctx); err != nil || len(all) != 0 {
		t.Errorf("Conversations = %+v, %v; want none", all, err)
	}
}

func TestSendRefusesAMessageWithNothingInIt(t *testing.T) {
	f := newFixture(t)
	// A provider that would answer, so what is refused is the message and not
	// the model it was for.
	if _, err := f.chat.AddProvider(ctx, "anthropic", "sk-ant-secret",
		answering(t, "hello"), []string{"claude-opus-5"}); err != nil {
		t.Fatalf("AddProvider: %v", err)
	}

	_, err := f.chat.Send(ctx, chat.SendRequest{Model: "claude-opus-5", Text: "   "}, nil)

	var invalid *chat.InvalidError
	if !errors.As(err, &invalid) {
		t.Fatalf("Send with nothing to say = %v, want it refused", err)
	}
}

// answering is a provider that says that, in the shape the Anthropic API
// streams it, and where to reach it.
func answering(t *testing.T, text string) string {
	t.Helper()
	piece, err := json.Marshal(text)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "event: message_start\ndata: {\"type\":\"message_start\"}\n\n"+
			"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,"+
			"\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n")
		_, _ = fmt.Fprintf(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\","+
			"\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":%s}}\n\n", piece)
		_, _ = fmt.Fprint(w, "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestAConversationsTitleIsCutAtAWholeCharacter(t *testing.T) {
	f := newFixture(t)
	if _, err := f.chat.AddProvider(ctx, "anthropic", "sk-ant-secret",
		answering(t, "understood"), []string{"claude-opus-5"}); err != nil {
		t.Fatalf("AddProvider: %v", err)
	}
	// Said in a script where a character is three bytes, and longer than a
	// title is, so the cut happens.
	said := strings.Repeat("設定を説明して", 30)

	if _, err := f.chat.Send(ctx, chat.SendRequest{Model: "claude-opus-5", Text: said}, nil); err != nil {
		t.Fatalf("Send: %v", err)
	}

	all, err := f.chat.Conversations(ctx)
	if err != nil || len(all) != 1 {
		t.Fatalf("Conversations = %+v, %v; want the one", all, err)
	}
	title := all[0].Title
	if !utf8.ValidString(title) || strings.ContainsRune(title, utf8.RuneError) {
		t.Errorf("the title %q is cut in the middle of a character", title)
	}
	// Shown in characters, not in bytes: a third of a title is not a title.
	if n := utf8.RuneCountInString(strings.TrimSuffix(title, "...")); n < 60 {
		t.Errorf("the title carries %d characters (%q), want what a list can show", n, title)
	}
}

// breakingStore is a store whose provider row cannot be written, which is what
// a rotation has to survive.
type breakingStore struct {
	chat.Store
	broken bool
}

func (s *breakingStore) AddChatProvider(ctx context.Context, p store.ChatProvider) error {
	if s.broken {
		return errors.New("the database is not having it")
	}
	return s.Store.AddChatProvider(ctx, p)
}

func TestAFailedRotationLeavesTheConfiguredProviderItsKey(t *testing.T) {
	f := newFixture(t)
	broken := &breakingStore{Store: f.store}
	service := chat.NewService(broken, f.creds, chat.NewTools(f.view))
	if _, err := service.AddProvider(ctx, "anthropic", "sk-ant-first", "", nil); err != nil {
		t.Fatalf("AddProvider: %v", err)
	}

	broken.broken = true
	if _, err := service.AddProvider(ctx, "anthropic", "sk-ant-second", "", nil); err == nil {
		t.Fatal("the rotation was reported as done though the row could not be written")
	}

	// The provider is still configured with what it was configured with, so
	// the key it reaches models with is the one it was reaching them with: a
	// rotation that did not happen has not half happened either.
	key, err := f.creds.Get(ctx, "chat/anthropic")
	if err != nil {
		t.Fatalf("the configured provider has no key left: %v", err)
	}
	if key != "sk-ant-first" {
		t.Errorf("the configured provider now reaches models with %q, want the key it had", key)
	}
}

func TestAFailedFirstConfigurationKeepsNoKey(t *testing.T) {
	f := newFixture(t)
	broken := &breakingStore{Store: f.store, broken: true}
	service := chat.NewService(broken, f.creds, chat.NewTools(f.view))

	if _, err := service.AddProvider(ctx, "anthropic", "sk-ant-orphan", "", nil); err == nil {
		t.Fatal("the provider was reported as configured though the row could not be written")
	}

	// Nothing is configured, so the key nothing can reach is taken back.
	if _, err := f.creds.Get(ctx, "chat/anthropic"); !errors.Is(err, credential.ErrNotFound) {
		t.Errorf("a key nothing reaches was left behind: %v", err)
	}
}

func TestWhatArrivedIsKeptWhenTheCallerGoesAway(t *testing.T) {
	f := newFixture(t)
	// A provider that says one piece and then says nothing more, so the only
	// thing that ends the answer is the caller going away.
	held := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "event: message_start\ndata: {\"type\":\"message_start\"}\n\n"+
			"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,"+
			"\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n"+
			"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,"+
			"\"delta\":{\"type\":\"text_delta\",\"text\":\"half an ans\"}}\n\n")
		w.(http.Flusher).Flush()
		<-held
	}))
	t.Cleanup(func() {
		close(held)
		srv.Close()
	})
	if _, err := f.chat.AddProvider(ctx, "anthropic", "sk-ant-secret",
		srv.URL, []string{"claude-opus-5"}); err != nil {
		t.Fatalf("AddProvider: %v", err)
	}
	going, gone := context.WithCancel(ctx)
	defer gone()

	id, err := f.chat.Send(going, chat.SendRequest{Model: "claude-opus-5", Text: "are you there"},
		func(d chat.Delta) error {
			if d.Text != "" {
				// The window closes while the answer is arriving.
				gone()
			}
			return nil
		})

	if err == nil {
		t.Fatal("Send reported an answer though the caller went away mid-stream")
	}
	_, said, err := f.chat.Conversation(ctx, id)
	if err != nil {
		t.Fatalf("the conversation could not be read back: %v", err)
	}
	var found bool
	for _, m := range said {
		if m.Role == "assistant" && strings.Contains(m.Text, "half an ans") {
			found = true
		}
	}
	if !found {
		t.Errorf("what the user watched arrive was not kept: %+v", said)
	}
}

// recording is a provider that answers, and keeps what it was asked, so what
// a conversation looks like on the wire can be looked at.
type recording struct {
	url  string
	mu   sync.Mutex
	sent []map[string]any
}

func recordingProvider(t *testing.T, text string) *recording {
	t.Helper()
	piece, err := json.Marshal(text)
	if err != nil {
		t.Fatal(err)
	}
	r := &recording{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		r.mu.Lock()
		r.sent = append(r.sent, body)
		r.mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "event: message_start\ndata: {\"type\":\"message_start\"}\n\n"+
			"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,"+
			"\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n")
		_, _ = fmt.Fprintf(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\","+
			"\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":%s}}\n\n", piece)
		_, _ = fmt.Fprint(w, "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
	}))
	t.Cleanup(srv.Close)
	r.url = srv.URL
	return r
}

// roles is what the last request carried, turn by turn, with what each said.
func (r *recording) roles(t *testing.T) ([]string, string) {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.sent) == 0 {
		t.Fatal("the provider was not asked anything")
	}
	body := r.sent[len(r.sent)-1]
	turns, ok := body["messages"].([]any)
	if !ok || len(turns) == 0 {
		t.Fatalf("the request carries no messages: %v", body)
	}
	var said []string
	for _, turn := range turns {
		m, ok := turn.(map[string]any)
		if !ok {
			t.Fatalf("a turn is not a turn: %v", turn)
		}
		role, _ := m["role"].(string)
		said = append(said, role)
	}
	return said, fmt.Sprint(turns)
}

// alternating fails when a conversation is not one the Anthropic API takes:
// it starts with the user, and no two turns in a row are the same role.
func alternating(t *testing.T, roles []string, said string) {
	t.Helper()
	// A conversation is both sides of it: turns merged away until one is left
	// would satisfy everything below and carry nothing back.
	if len(roles) < 2 {
		t.Errorf("the conversation went as %v, want the turns it was made of\n%s", roles, tailOf(said))
		return
	}
	var user, assistant bool
	for _, role := range roles {
		user = user || role == "user"
		assistant = assistant || role == "assistant"
	}
	if !user || !assistant {
		t.Errorf("the conversation went as %v, want both sides of it\n%s", roles, tailOf(said))
	}
	if roles[0] != "user" {
		t.Errorf("the conversation starts with %q, want the user: %v\n%s", roles[0], roles, tailOf(said))
	}
	for at := 1; at < len(roles); at++ {
		if roles[at] == roles[at-1] {
			t.Errorf("two %s turns in a row at %d: %v\n%s", roles[at], at, roles, tailOf(said))
			break
		}
	}
}

// tailOf is the end of what was sent, for a report a person reads.
func tailOf(said string) string {
	if len(said) <= 800 {
		return said
	}
	return "..." + said[len(said)-800:]
}

func TestAnAnswerThatNeverArrivedDoesNotSpoilTheNextOne(t *testing.T) {
	f := newFixture(t)
	p := recordingProvider(t, "this one answers")
	if _, err := f.chat.AddProvider(ctx, "anthropic", "sk-ant-secret",
		p.url, []string{"claude-opus-5"}); err != nil {
		t.Fatalf("AddProvider: %v", err)
	}
	// A conversation the provider failed in the middle of: the question is
	// there and nothing ever answered it.
	now := time.Now().UTC()
	c, err := f.store.StartConversation(ctx, store.Conversation{
		Title: "the first question", Model: "claude-opus-5", Created: now, Updated: now,
	})
	if err != nil {
		t.Fatalf("StartConversation: %v", err)
	}
	for at, turn := range []struct{ role, text string }{
		{"user", "the first question"},
		{"assistant", "the first answer"},
		// This one nothing ever answered.
		{"user", "the second question"},
	} {
		if _, err := f.store.AddChatMessage(ctx, store.ChatMessage{
			ConversationID: c.ID, Role: turn.role, Text: turn.text,
			Created: now.Add(time.Duration(at) * time.Second),
		}); err != nil {
			t.Fatalf("AddChatMessage: %v", err)
		}
	}

	if _, err := f.chat.Send(ctx, chat.SendRequest{
		Conversation: c.ID, Model: "claude-opus-5", Text: "the third question",
	}, nil); err != nil {
		t.Fatalf("Send: %v", err)
	}

	roles, said := p.roles(t)
	alternating(t, roles, said)
	for _, want := range []string{"the first question", "the first answer", "the second question", "the third question"} {
		if !strings.Contains(said, want) {
			t.Errorf("what was sent does not carry %q:\n%s", want, said)
		}
	}
}

func TestAConversationCutToItsRecentTurnsStillStartsWithTheUser(t *testing.T) {
	f := newFixture(t)
	p := recordingProvider(t, "understood")
	if _, err := f.chat.AddProvider(ctx, "anthropic", "sk-ant-secret",
		p.url, []string{"claude-opus-5"}); err != nil {
		t.Fatalf("AddProvider: %v", err)
	}
	now := time.Now().UTC()
	c, err := f.store.StartConversation(ctx, store.Conversation{
		Title: "a long conversation", Model: "claude-opus-5", Created: now, Updated: now,
	})
	if err != nil {
		t.Fatalf("StartConversation: %v", err)
	}
	// Long enough that only its most recent turns are carried, and turns big
	// enough that where the cut lands is not the user's turn.
	long := strings.Repeat("x", 26000)
	for at := range 10 {
		role := "user"
		if at%2 == 1 {
			role = "assistant"
		}
		if _, err := f.store.AddChatMessage(ctx, store.ChatMessage{
			ConversationID: c.ID, Role: role, Text: fmt.Sprintf("turn %d %s", at, long),
			Created: now.Add(time.Duration(at) * time.Second),
		}); err != nil {
			t.Fatalf("AddChatMessage: %v", err)
		}
	}

	if _, err := f.chat.Send(ctx, chat.SendRequest{
		Conversation: c.ID, Model: "claude-opus-5", Text: "and one more thing",
	}, nil); err != nil {
		t.Fatalf("Send: %v", err)
	}

	roles, said := p.roles(t)
	alternating(t, roles, said)
	if len(said) > maxCarried {
		t.Errorf("the request carries %d bytes, want the recent turns and no more", len(said))
	}
	if !strings.Contains(said, "and one more thing") {
		t.Error("what was sent does not carry the question that was asked")
	}
}

// maxCarried is what a request may carry of a conversation, with room for the
// turn markers around the text itself.
const maxCarried = 160 << 10

// recordingOpenRouter is a provider that speaks the OpenAI shape OpenRouter
// speaks, and keeps what it was asked.
func recordingOpenRouter(t *testing.T, text string) *recording {
	t.Helper()
	piece, err := json.Marshal(text)
	if err != nil {
		t.Fatal(err)
	}
	r := &recording{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		r.mu.Lock()
		r.sent = append(r.sent, body)
		r.mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":%s}}]}\n\n", piece)
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)
	r.url = srv.URL
	return r
}

func TestEveryProviderIsSentAConversationInTheSameShape(t *testing.T) {
	f := newFixture(t)
	p := recordingOpenRouter(t, "this one answers")
	if _, err := f.chat.AddProvider(ctx, "openrouter", "sk-or-secret",
		p.url, []string{"anthropic/claude-sonnet-4.5"}); err != nil {
		t.Fatalf("AddProvider: %v", err)
	}
	// The same conversation that would be refused: a question nothing
	// answered, asked again. What a model behind OpenRouter makes of it is
	// OpenRouter's business, and it is sent the shape every model takes.
	now := time.Now().UTC()
	c, err := f.store.StartConversation(ctx, store.Conversation{
		Title: "the first question", Model: "anthropic/claude-sonnet-4.5", Created: now, Updated: now,
	})
	if err != nil {
		t.Fatalf("StartConversation: %v", err)
	}
	for at, turn := range []struct{ role, text string }{
		// An answer with nothing before it, as a conversation cut to its most
		// recent turns begins.
		{"assistant", "an answer to something older"},
		{"user", "the first question"},
		{"assistant", "the first answer"},
		{"user", "the second question"},
	} {
		if _, err := f.store.AddChatMessage(ctx, store.ChatMessage{
			ConversationID: c.ID, Role: turn.role, Text: turn.text,
			Created: now.Add(time.Duration(at) * time.Second),
		}); err != nil {
			t.Fatalf("AddChatMessage: %v", err)
		}
	}

	if _, err := f.chat.Send(ctx, chat.SendRequest{
		Conversation: c.ID, Model: "anthropic/claude-sonnet-4.5", Text: "the third question",
	}, nil); err != nil {
		t.Fatalf("Send: %v", err)
	}

	roles, said := p.roles(t)
	// What that provider is told it is comes first, and is not a turn of the
	// conversation.
	if roles[0] != "system" {
		t.Fatalf("the request starts with %q, want what the model is told it is: %v", roles[0], roles)
	}
	alternating(t, roles[1:], said)
	if strings.Contains(said, "an answer to something older") {
		t.Errorf("the turn the conversation could not start with was carried anyway:\n%s", tailOf(said))
	}
	for _, want := range []string{"the first question", "the second question", "the third question"} {
		if !strings.Contains(said, want) {
			t.Errorf("what was sent does not carry %q:\n%s", want, tailOf(said))
		}
	}
}
