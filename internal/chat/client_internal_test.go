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

	var said, results int
	for _, m := range got {
		switch m["role"] {
		case "tool":
			results++
		case RoleUser:
			if m["content"] == "and what did it print" {
				said++
			}
		}
	}
	if results != 1 {
		t.Errorf("the request carries %d tool results, want the one that was answered: %+v", results, got)
	}
	if said != 1 {
		t.Errorf("what the user said beside the result is not carried: %+v", got)
	}
}
