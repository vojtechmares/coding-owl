package chat_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vojtechmares/coding-owl/internal/chat"
	"github.com/vojtechmares/coding-owl/internal/store"
)

// provider is an HTTP server standing in for a model provider: it keeps what
// it was sent and answers with the stream the test gives it.
type provider struct {
	server *httptest.Server
	body   map[string]any
	header http.Header
	status int
}

func newProvider(t *testing.T, stream string) *provider {
	t.Helper()
	p := &provider{status: http.StatusOK}
	p.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.header = r.Header.Clone()
		_ = json.NewDecoder(r.Body).Decode(&p.body)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(p.status)
		_, _ = w.Write([]byte(stream))
	}))
	t.Cleanup(p.server.Close)
	return p
}

// client is a client for that provider against this server.
func (p *provider) client(t *testing.T, name, key string) chat.Client {
	t.Helper()
	c, err := chat.NewClient(store.ChatProvider{Name: name, BaseURL: p.server.URL}, key)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c
}

// collect is what a client streamed and what it said in the end.
func collect(t *testing.T, c chat.Client, req chat.Request) ([]string, chat.Reply, error) {
	t.Helper()
	var pieces []string
	reply, err := c.Stream(ctx, req, func(piece string) error {
		pieces = append(pieces, piece)
		return nil
	})
	return pieces, reply, err
}

func TestAnthropicStreamsThePiecesAsTheyArrive(t *testing.T) {
	p := newProvider(t, strings.Join([]string{
		`event: message_start`,
		`data: {"type":"message_start"}`,
		``,
		`event: content_block_start`,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		``,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Job 7 "}}`,
		``,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"was blocked."}}`,
		``,
		`event: message_stop`,
		`data: {"type":"message_stop"}`,
		``,
	}, "\n"))

	pieces, reply, err := collect(t, p.client(t, chat.Anthropic, "sk-ant-key"), chat.Request{
		Model: "claude-opus-5", System: "you are owl",
		Messages: []chat.Turn{{Role: chat.RoleUser, Text: "why was job 7 blocked"}},
		Tools:    []chat.ToolDefinition{{Name: "get_job", Description: "one job", Schema: map[string]any{"type": "object"}}},
	})

	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if len(pieces) != 2 || reply.Text != "Job 7 was blocked." {
		t.Errorf("streamed %v, assembled %q", pieces, reply.Text)
	}
	if p.header.Get("x-api-key") != "sk-ant-key" {
		t.Errorf("the key was sent as %q", p.header.Get("x-api-key"))
	}
	if p.header.Get("anthropic-version") == "" {
		t.Error("no api version was pinned")
	}
	if p.body["system"] != "you are owl" {
		t.Errorf("the system prompt was sent as %v", p.body["system"])
	}
	if p.body["stream"] != true {
		t.Errorf("the request did not ask for a stream: %v", p.body)
	}
	tools, _ := p.body["tools"].([]any)
	if len(tools) != 1 {
		t.Errorf("the request carried %v as tools", p.body["tools"])
	}
}

func TestAnthropicReadsAToolCallOutOfItsPieces(t *testing.T) {
	p := newProvider(t, strings.Join([]string{
		`event: content_block_start`,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"call-1","name":"get_job"}}`,
		``,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"id\":"}}`,
		``,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"7}"}}`,
		``,
		`event: content_block_stop`,
		`data: {"type":"content_block_stop","index":0}`,
		``,
		`event: message_stop`,
		`data: {"type":"message_stop"}`,
		``,
	}, "\n"))

	_, reply, err := collect(t, p.client(t, chat.Anthropic, "k"), chat.Request{Model: "claude-opus-5"})

	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if len(reply.ToolCalls) != 1 {
		t.Fatalf("Stream = %+v, want the tool call it asked for", reply)
	}
	call := reply.ToolCalls[0]
	if call.ID != "call-1" || call.Name != "get_job" || string(call.Input) != `{"id":7}` {
		t.Errorf("the tool call is %+v, want it assembled from its pieces", call)
	}
}

func TestAnthropicReportsWhatTheProviderRefused(t *testing.T) {
	p := newProvider(t, `event: error
data: {"type":"error","error":{"type":"overloaded_error","message":"the model is overloaded"}}

`)

	_, _, err := collect(t, p.client(t, chat.Anthropic, "k"), chat.Request{Model: "claude-opus-5"})

	if err == nil {
		t.Fatal("Stream of a refusal = nil, want the refusal reported")
	}
	if !strings.Contains(err.Error(), "overloaded") {
		t.Errorf("the error %q does not say what the provider said", err)
	}
}

func TestAClientReportsAStatusItCannotUse(t *testing.T) {
	p := newProvider(t, "nope")
	p.status = http.StatusUnauthorized

	_, _, err := collect(t, p.client(t, chat.Anthropic, "k"), chat.Request{Model: "claude-opus-5"})

	if err == nil {
		t.Fatal("Stream against a refusal = nil, want an error")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("the error %q does not carry the status", err)
	}
}

func TestOpenRouterStreamsThePiecesAndAssemblesToolCalls(t *testing.T) {
	p := newProvider(t, strings.Join([]string{
		`data: {"choices":[{"delta":{"content":"It was "}}]}`,
		``,
		`data: {"choices":[{"delta":{"content":"the tests."}}]}`,
		``,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call-1","function":{"name":"get_job","arguments":"{\"id\":"}}]}}]}`,
		``,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"7}"}}]}}]}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n"))

	pieces, reply, err := collect(t, p.client(t, chat.OpenRouter, "sk-or-key"), chat.Request{
		Model: "openai/gpt-5", System: "you are owl",
		Messages: []chat.Turn{
			{Role: chat.RoleUser, Text: "why was job 7 blocked"},
			{Role: chat.RoleAssistant, ToolCalls: []chat.ToolCall{{ID: "c", Name: "get_job", Input: []byte(`{"id":7}`)}}},
			{Role: chat.RoleUser, ToolResults: []chat.ToolResult{{CallID: "c", Text: "job 7: blocked"}}},
		},
	})

	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if len(pieces) != 2 || reply.Text != "It was the tests." {
		t.Errorf("streamed %v, assembled %q", pieces, reply.Text)
	}
	if len(reply.ToolCalls) != 1 || string(reply.ToolCalls[0].Input) != `{"id":7}` {
		t.Errorf("tool calls = %+v, want one assembled from its pieces", reply.ToolCalls)
	}
	if p.header.Get("Authorization") != "Bearer sk-or-key" {
		t.Errorf("the key was sent as %q", p.header.Get("Authorization"))
	}
	// The system prompt is a turn of its own in this shape, and a tool result
	// is addressed to the call it answers.
	messages, _ := p.body["messages"].([]any)
	if len(messages) != 4 {
		t.Fatalf("the request carried %d messages: %v", len(messages), messages)
	}
	first, _ := messages[0].(map[string]any)
	last, _ := messages[3].(map[string]any)
	if first["role"] != "system" || last["role"] != "tool" || last["tool_call_id"] != "c" {
		t.Errorf("the conversation was sent as %v", messages)
	}
}
