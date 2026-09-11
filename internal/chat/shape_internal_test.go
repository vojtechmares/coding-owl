package chat

import (
	"reflect"
	"testing"
)

// shaped is what makes a conversation one a provider will take. These are the
// shapes it has to make out of what a conversation can hold, including what it
// will hold when a Conversation keeps the tools a model asked for.
func TestShapedMakesAConversationAProviderWillTake(t *testing.T) {
	for what, c := range map[string]struct{ in, want []Turn }{
		"a conversation that starts with an answer": {
			in: []Turn{
				{Role: RoleAssistant, Text: "an answer to something older"},
				{Role: RoleUser, Text: "a question"},
			},
			want: []Turn{{Role: RoleUser, Text: "a question"}},
		},
		"a question nothing answered": {
			in: []Turn{
				{Role: RoleUser, Text: "the first question"},
				{Role: RoleUser, Text: "the second question"},
			},
			want: []Turn{{Role: RoleUser, Text: "the first question\n\nthe second question"}},
		},
		"a turn that asked for a tool, merged": {
			in: []Turn{
				{Role: RoleUser, Text: "a question"},
				{Role: RoleAssistant, Text: "one moment", ToolCalls: []ToolCall{{ID: "call-1", Name: "list_jobs"}}},
				{Role: RoleAssistant, Text: "and another", ToolCalls: []ToolCall{{ID: "call-2", Name: "list_projects"}}},
			},
			want: []Turn{
				{Role: RoleUser, Text: "a question"},
				{Role: RoleAssistant, Text: "one moment\n\nand another", ToolCalls: []ToolCall{
					{ID: "call-1", Name: "list_jobs"}, {ID: "call-2", Name: "list_projects"},
				}},
			},
		},
		"a result whose call was dropped for starting the conversation": {
			in: []Turn{
				{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "call-1", Name: "list_jobs"}}},
				{Role: RoleUser, ToolResults: []ToolResult{{CallID: "call-1", Text: "two jobs"}}},
				{Role: RoleUser, Text: "a question"},
			},
			want: []Turn{{Role: RoleUser, Text: "a question"}},
		},
		"a result whose call is still there": {
			in: []Turn{
				{Role: RoleAssistant, Text: "an answer to something older"},
				{Role: RoleUser, Text: "a question"},
				{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "call-1", Name: "list_jobs"}}},
				{Role: RoleUser, ToolResults: []ToolResult{{CallID: "call-1", Text: "two jobs"}}},
			},
			want: []Turn{
				{Role: RoleUser, Text: "a question"},
				{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "call-1", Name: "list_jobs"}}},
				{Role: RoleUser, ToolResults: []ToolResult{{CallID: "call-1", Text: "two jobs"}}},
			},
		},
	} {
		if got := shaped(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s:\n got %+v\nwant %+v", what, got, c.want)
		}
	}
}
