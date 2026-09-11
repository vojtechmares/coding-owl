package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// anthropicURL is where the Anthropic API lives when a provider names no other.
const anthropicURL = "https://api.anthropic.com"

// anthropicVersion is the API version header that API requires. It is a date,
// and pinning it is what keeps a change to the API from arriving unannounced.
const anthropicVersion = "2023-06-01"

// maxTokens bounds one answer. The API requires a number, and a chat answer
// about a Job is not a long document.
const maxTokens = 4096

// anthropicClient speaks the Anthropic Messages API.
type anthropicClient struct {
	key     string
	baseURL string
	http    *http.Client
}

// Stream sends one exchange and reads the event stream back.
func (c *anthropicClient) Stream(ctx context.Context, req Request, emit func(string) error) (Reply, error) {
	body := map[string]any{
		"model":      req.Model,
		"max_tokens": maxTokens,
		"stream":     true,
		"messages":   anthropicMessages(req.Messages),
	}
	if req.System != "" {
		body["system"] = req.System
	}
	if len(req.Tools) > 0 {
		tools := make([]map[string]any, 0, len(req.Tools))
		for _, t := range req.Tools {
			tools = append(tools, map[string]any{
				"name": t.Name, "description": t.Description, "input_schema": t.Schema,
			})
		}
		body["tools"] = tools
	}
	res, err := post(ctx, c.http, c.baseURL+"/v1/messages", map[string]string{
		"x-api-key":         c.key,
		"anthropic-version": anthropicVersion,
	}, body)
	if err != nil {
		return Reply{}, err
	}
	defer func() { _ = res.Body.Close() }()

	var reply Reply
	// A tool call arrives as a block that is opened, filled in pieces, and
	// closed, so the one being filled is held until it closes.
	var call *ToolCall
	var input strings.Builder
	err = events(res.Body, func(data []byte) error {
		var ev struct {
			Type         string `json:"type"`
			Index        int    `json:"index"`
			ContentBlock struct {
				Type string `json:"type"`
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"content_block"`
			Delta struct {
				Type        string `json:"type"`
				Text        string `json:"text"`
				PartialJSON string `json:"partial_json"`
			} `json:"delta"`
			Error struct {
				Type    string `json:"type"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(data, &ev); err != nil {
			// A stream Owl cannot read is a failure of the exchange, not
			// something to answer from.
			return fmt.Errorf("the provider sent an event Owl could not read: %v", err)
		}
		switch ev.Type {
		case "content_block_start":
			if ev.ContentBlock.Type == "tool_use" {
				call = &ToolCall{ID: ev.ContentBlock.ID, Name: ev.ContentBlock.Name}
				input.Reset()
			}
		case "content_block_delta":
			switch ev.Delta.Type {
			case "text_delta":
				if ev.Delta.Text != "" {
					reply.Text += ev.Delta.Text
					return emit(ev.Delta.Text)
				}
			case "input_json_delta":
				input.WriteString(ev.Delta.PartialJSON)
			}
		case "content_block_stop":
			if call != nil {
				call.Input = json.RawMessage(orEmptyObject(input.String()))
				reply.ToolCalls = append(reply.ToolCalls, *call)
				call, input = nil, strings.Builder{}
			}
		case "error":
			return fmt.Errorf("the provider refused: %s", strings.TrimSpace(ev.Error.Message))
		}
		return nil
	})
	if err != nil {
		return reply, err
	}
	return reply, nil
}

// anthropicMessages is the conversation as that API takes it: text, tool calls
// and tool results are all content blocks on a turn. It arrives in the shape
// that API requires - starting with the user, one turn to a role - because
// open shapes it there, for every provider.
func anthropicMessages(turns []Turn) []map[string]any {
	out := make([]map[string]any, 0, len(turns))
	for _, t := range turns {
		var content []map[string]any
		if strings.TrimSpace(t.Text) != "" {
			content = append(content, map[string]any{"type": "text", "text": t.Text})
		}
		for _, call := range t.ToolCalls {
			content = append(content, map[string]any{
				"type": "tool_use", "id": call.ID, "name": call.Name,
				"input": json.RawMessage(orEmptyObject(string(call.Input))),
			})
		}
		for _, result := range t.ToolResults {
			content = append(content, map[string]any{
				"type": "tool_result", "tool_use_id": result.CallID,
				"content": result.Text, "is_error": result.Failed,
			})
		}
		if len(content) == 0 {
			continue
		}
		out = append(out, map[string]any{"role": t.Role, "content": content})
	}
	return out
}

// orEmptyObject is arguments as JSON, for a call the model sent none for.
func orEmptyObject(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return "{}"
	}
	return raw
}
