package chat

import "testing"

// reachedAt is what an error says about where a request went. What a url
// carries beyond its path is the user's own text, and an error is shown.
func TestReachedAtSaysWhereWithoutSayingWhat(t *testing.T) {
	for raw, want := range map[string]string{
		"https://api.anthropic.com/v1/messages":          "https://api.anthropic.com/v1/messages",
		"https://models.test/v1?token=s3cr3t":            "https://models.test/v1",
		"https://models.test/v1#s3cr3t":                  "https://models.test/v1",
		"https://someone:s3cr3t@models.test/v1/messages": "https://models.test/v1/messages",
		"https://models.test/a%0d%0ab/v1":                "https://models.test/a%0d%0ab/v1",
		"https://127.0.0.1:8080/v1/messages":             "https://127.0.0.1:8080/v1/messages",
	} {
		if got := reachedAt(raw); got != want {
			t.Errorf("reachedAt(%q) = %q, want %q", raw, got, want)
		}
	}
	if got := reachedAt("://not a url"); got == "" {
		t.Errorf("reachedAt of something unreadable = %q, want something a person can read", got)
	}
}

// openAIMessages carries a turn that says something as well as answering a
// tool call: shaped merges two turns of one role into one, and what the user
// said is not a tool result to be dropped beside one.
func TestOpenAIMessagesCarriesWhatATurnSaidBesideItsResults(t *testing.T) {
	got := openAIMessages("", []Turn{
		{Role: RoleUser, Text: "a question"},
		{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "call-1", Name: "list_jobs"}}},
		{Role: RoleUser, Text: "and what did it print", ToolResults: []ToolResult{
			{CallID: "call-1", Text: "two jobs"},
		}},
	})

	var results, said, calls int
	results, said, calls = -1, -1, -1
	for at, m := range got {
		switch {
		case m["role"] == "tool":
			results = at
		case m["tool_calls"] != nil:
			calls = at
		case m["role"] == RoleUser && m["content"] == "and what did it print":
			said = at
		}
	}
	if results < 0 {
		t.Fatalf("the request carries no tool result: %+v", got)
	}
	if said < 0 {
		t.Fatalf("what the user said beside the result is not carried: %+v", got)
	}
	// That API takes a tool's answer straight after the turn that asked for
	// it, so what was said beside it comes after, not between.
	if calls+1 != results || results >= said {
		t.Errorf("the turns go call=%d result=%d said=%d, want the result straight after the call: %+v",
			calls, results, said, got)
	}
}
