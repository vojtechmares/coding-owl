package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// openRouterURL is where OpenRouter lives when a provider names no other.
const openRouterURL = "https://openrouter.ai/api/v1"

// openAIClient speaks the OpenAI chat completions shape, which is what
// OpenRouter offers for every model it carries (ADR-0022).
type openAIClient struct {
	key     string
	baseURL string
	http    *http.Client
}

// Stream sends one exchange and reads the event stream back.
func (c *openAIClient) Stream(ctx context.Context, req Request, emit func(string) error) (Reply, error) {
	body := map[string]any{
		"model":    req.Model,
		"stream":   true,
		"messages": openAIMessages(req.System, req.Messages),
	}
	if len(req.Tools) > 0 {
		tools := make([]map[string]any, 0, len(req.Tools))
		for _, t := range req.Tools {
			tools = append(tools, map[string]any{
				"type": "function",
				"function": map[string]any{
					"name": t.Name, "description": t.Description, "parameters": t.Schema,
				},
			})
		}
		body["tools"] = tools
	}
	res, err := post(ctx, c.http, c.baseURL+"/chat/completions", map[string]string{
		"Authorization": "Bearer " + c.key,
	}, body)
	if err != nil {
		return Reply{}, err
	}
	defer func() { _ = res.Body.Close() }()

	var reply Reply
	// Tool calls arrive in pieces, addressed by their place in the list.
	calls := map[int]*ToolCall{}
	var order []int
	err = events(res.Body, func(data []byte) error {
		var ev struct {
			Choices []struct {
				Delta struct {
					Content   string `json:"content"`
					ToolCalls []struct {
						Index    int    `json:"index"`
						ID       string `json:"id"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
			} `json:"choices"`
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(data, &ev); err != nil {
			return invalid("the provider sent an event Owl could not read: %v", err)
		}
		if strings.TrimSpace(ev.Error.Message) != "" {
			return fmt.Errorf("the provider refused: %s", strings.TrimSpace(ev.Error.Message))
		}
		for _, choice := range ev.Choices {
			if text := choice.Delta.Content; text != "" {
				reply.Text += text
				if err := emit(text); err != nil {
					return err
				}
			}
			for _, call := range choice.Delta.ToolCalls {
				held, ok := calls[call.Index]
				if !ok {
					held = &ToolCall{}
					calls[call.Index] = held
					order = append(order, call.Index)
				}
				if call.ID != "" {
					held.ID = call.ID
				}
				if call.Function.Name != "" {
					held.Name = call.Function.Name
				}
				held.Input = append(held.Input, call.Function.Arguments...)
			}
		}
		return nil
	})
	if err != nil {
		return reply, err
	}
	for _, index := range order {
		call := calls[index]
		call.Input = json.RawMessage(orEmptyObject(string(call.Input)))
		reply.ToolCalls = append(reply.ToolCalls, *call)
	}
	return reply, nil
}

// openAIMessages is the conversation as that shape takes it: the system prompt
// is a turn of its own, and a tool result is a turn addressed to its call.
func openAIMessages(system string, turns []Turn) []map[string]any {
	out := make([]map[string]any, 0, len(turns)+1)
	if strings.TrimSpace(system) != "" {
		out = append(out, map[string]any{"role": "system", "content": system})
	}
	for _, t := range turns {
		switch {
		case len(t.ToolResults) > 0:
			for _, result := range t.ToolResults {
				out = append(out, map[string]any{
					"role": "tool", "tool_call_id": result.CallID, "content": result.Text,
				})
			}
		case len(t.ToolCalls) > 0:
			calls := make([]map[string]any, 0, len(t.ToolCalls))
			for _, call := range t.ToolCalls {
				calls = append(calls, map[string]any{
					"id": call.ID, "type": "function",
					"function": map[string]any{
						"name": call.Name, "arguments": orEmptyObject(string(call.Input)),
					},
				})
			}
			out = append(out, map[string]any{
				"role": RoleAssistant, "content": t.Text, "tool_calls": calls,
			})
		case strings.TrimSpace(t.Text) != "":
			out = append(out, map[string]any{"role": t.Role, "content": t.Text})
		}
	}
	return out
}
