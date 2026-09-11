package chat

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/vojtechmares/coding-owl/internal/store"
)

// requestTimeout bounds one exchange with a provider. A model that thinks for
// longer than this has stopped answering, and a daemon nobody is watching must
// not hold a stream open for ever.
const requestTimeout = 5 * time.Minute

// maxEvent bounds one event of a provider's stream, which is a line.
const maxEvent = 1 << 20

// Turn is one thing said in a conversation as a provider is told it: what the
// user or the model said, what the model asked for, and what the tools
// answered.
type Turn struct {
	// Role is who said it.
	Role string
	// Text is what was said, empty for a turn that is only tool calls or
	// results.
	Text string
	// ToolCalls are what the model asked for, on an assistant turn.
	ToolCalls []ToolCall
	// ToolResults are what Owl answered, on the user turn that follows.
	ToolResults []ToolResult
}

// ToolCall is the model asking for one tool.
type ToolCall struct {
	// ID is the provider's own name for this call, which the result carries
	// back.
	ID string
	// Name is the tool asked for.
	Name string
	// Input is the arguments, as the model wrote them.
	Input json.RawMessage
}

// ToolResult is what Owl answered one call with.
type ToolResult struct {
	// CallID is the call it answers.
	CallID string
	// Text is the answer, or why there is none.
	Text string
	// Failed says the answer is why it could not be done.
	Failed bool
}

// ToolDefinition is a tool as a provider is told about it.
type ToolDefinition struct {
	Name        string
	Description string
	// Schema is the JSON schema of its arguments.
	Schema map[string]any
}

// Request is one exchange with a provider.
type Request struct {
	Model    string
	System   string
	Messages []Turn
	Tools    []ToolDefinition
}

// Reply is what a model said in one exchange.
type Reply struct {
	// Text is what it said, which has already been streamed piece by piece.
	Text string
	// ToolCalls are what it asked for instead of answering.
	ToolCalls []ToolCall
}

// Client is one provider Owl can speak to.
type Client interface {
	// Stream sends the exchange and calls emit for each piece of text as it
	// arrives. The Reply is what the model said in the end.
	Stream(ctx context.Context, req Request, emit func(text string) error) (Reply, error)
}

// NewClient is the client for a configured provider.
func NewClient(p store.ChatProvider, key string) (Client, error) {
	http := &http.Client{Timeout: requestTimeout}
	switch p.Name {
	case Anthropic:
		return &anthropicClient{key: key, baseURL: orDefault(p.BaseURL, anthropicURL), http: http}, nil
	case OpenRouter:
		return &openAIClient{key: key, baseURL: orDefault(p.BaseURL, openRouterURL), http: http}, nil
	default:
		return nil, invalid("%q is not a provider Owl drives", p.Name)
	}
}

func orDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimRight(strings.TrimSpace(value), "/")
}

// post sends a request body and returns the response to read the stream from.
func post(ctx context.Context, c *http.Client, url string, header map[string]string, body any) (*http.Response, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(raw)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	for name, value := range header {
		req.Header.Set(name, value)
	}
	res, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode/100 != 2 {
		defer func() { _ = res.Body.Close() }()
		// What a provider says about a refusal is worth reading, but it is
		// somebody else's text: only the beginning of it is carried.
		message, _ := io.ReadAll(io.LimitReader(res.Body, 2<<10))
		return nil, fmt.Errorf("%s said %s: %s", url, res.Status, strings.TrimSpace(string(message)))
	}
	return res, nil
}

// events reads a server-sent event stream, calling on for each `data:` payload
// in order. It stops at the end of the stream, or at the first error.
func events(body io.Reader, on func(data []byte) error) error {
	sc := bufio.NewScanner(body)
	sc.Buffer(make([]byte, 0, 64<<10), maxEvent)
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		data, ok := strings.CutPrefix(line, "data:")
		if !ok {
			// `event:` lines and blank lines carry nothing Owl reads: the
			// payload says what it is.
			continue
		}
		data = strings.TrimSpace(data)
		if data == "" || data == "[DONE]" {
			continue
		}
		if err := on([]byte(data)); err != nil {
			return err
		}
	}
	return sc.Err()
}
