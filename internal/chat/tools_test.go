package chat_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/vojtechmares/coding-owl/internal/chat"
)

// call is one tool call as a model would make it.
func call(name string, input any) chat.ToolCall {
	raw, _ := json.Marshal(input)
	return chat.ToolCall{ID: "call-1", Name: name, Input: raw}
}

func TestToolsOfferOnlyReads(t *testing.T) {
	tools := chat.NewTools(&fakeView{})

	defs := tools.Definitions()

	names := map[string]bool{}
	for _, d := range defs {
		names[d.Name] = true
		if strings.TrimSpace(d.Description) == "" {
			t.Errorf("%s is offered with no description", d.Name)
		}
		if d.Schema == nil || d.Schema["type"] != "object" {
			t.Errorf("%s is offered with the schema %v", d.Name, d.Schema)
		}
	}
	for _, want := range []string{"list_projects", "list_jobs", "get_job", "read_run_log", "get_job_diff"} {
		if !names[want] {
			t.Errorf("the tools are %v, want %s among them", names, want)
		}
	}
	if len(defs) != 5 {
		t.Errorf("the tools are %v; anything beyond reading is not one of them", names)
	}
}

func TestToolsAnswerFromWhatOwlKnows(t *testing.T) {
	view := &fakeView{
		projects: "api\tpath=/repos/api",
		jobs:     "job 1\tstate=pending",
		job:      "job 1\nstate: blocked",
		log:      "working on it",
		diff:     "+a line the agent added",
	}
	tools := chat.NewTools(view)

	for _, c := range []struct {
		call chat.ToolCall
		want string
	}{
		{call("list_projects", nil), "api"},
		{call("list_jobs", map[string]any{"all": true}), "job 1"},
		{call("get_job", map[string]any{"id": 1}), "blocked"},
		// A model that writes an id as a string means the same id.
		{call("read_run_log", map[string]any{"run_id": "2"}), "working on it"},
		{call("get_job_diff", map[string]any{"id": 1}), "a line the agent added"},
	} {
		got := tools.Call(ctx, c.call)

		if got.Failed || !strings.Contains(got.Text, c.want) {
			t.Errorf("%s answered %+v, want it to carry %q", c.call.Name, got, c.want)
		}
		if got.CallID != "call-1" {
			t.Errorf("%s answered call %q", c.call.Name, got.CallID)
		}
	}
	if len(view.asked) != 5 {
		t.Errorf("Owl was asked %v, want one read per call", view.asked)
	}
}

func TestToolsRefuseWhatOwlDoesNotHave(t *testing.T) {
	tools := chat.NewTools(&fakeView{})

	got := tools.Call(ctx, call("run_command", map[string]any{"argv": []string{"rm", "-rf", "/"}}))

	if !got.Failed {
		t.Errorf("a tool Owl does not have answered %+v", got)
	}
	for _, want := range []string{"run_command", "not a tool"} {
		if !strings.Contains(got.Text, want) {
			t.Errorf("the refusal %q does not say %q", got.Text, want)
		}
	}
}

func TestToolsRefuseACallWithoutWhatItNeeds(t *testing.T) {
	tools := chat.NewTools(&fakeView{})

	for what, c := range map[string]chat.ToolCall{
		"no id at all":       call("get_job", map[string]any{}),
		"an id that is not":  call("get_job", map[string]any{"id": "the first one"}),
		"arguments that are": {ID: "x", Name: "list_jobs", Input: []byte("not json")},
	} {
		got := tools.Call(ctx, c)

		if !got.Failed {
			t.Errorf("%s answered %+v, want the model told why not", what, got)
		}
	}
}

func TestToolsCarryOnlySoMuch(t *testing.T) {
	long := strings.Repeat("a line of a very long log\n", 5000)
	tools := chat.NewTools(&fakeView{log: long})

	got := tools.Call(ctx, call("read_run_log", map[string]any{"run_id": 1}))

	if len(got.Text) >= len(long) {
		t.Errorf("the answer is %d bytes of %d, want it bounded", len(got.Text), len(long))
	}
	if !strings.Contains(got.Text, "not carried") {
		t.Errorf("what was left out is not said: %q", got.Text[max(len(got.Text)-120, 0):])
	}
}
