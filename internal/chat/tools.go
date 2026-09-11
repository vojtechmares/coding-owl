package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// maxResult bounds one tool's answer. It goes back to the provider inside the
// next request, so a log or a diff is carried as much of as a reader needs and
// no more.
const maxResult = 16 << 10

// Names of the tools. They only read: what Owl knows, not what it does
// (ADR-0022).
const (
	toolListProjects = "list_projects"
	toolListJobs     = "list_jobs"
	toolGetJob       = "get_job"
	toolReadRunLog   = "read_run_log"
	toolGetJobDiff   = "get_job_diff"
)

// View is what the chat may read. Every method answers from what the daemon
// already holds, and none of them changes anything: the chat never acts.
type View interface {
	// Projects is every registered Project, as `owl project list` reports it.
	Projects(ctx context.Context) (string, error)
	// Jobs is the queue, or every Job whatever its state.
	Jobs(ctx context.Context, all bool) (string, error)
	// Job is one Job with its Runs, what Verification said, its plan and its
	// handoff, as `owl jobs show` reports it.
	Job(ctx context.Context, id int64) (string, error)
	// RunLog is the end of a Run's captured output, at most max bytes.
	RunLog(ctx context.Context, id int64, max int) (string, error)
	// JobDiff is what a Job's branch changed, at most max bytes.
	JobDiff(ctx context.Context, id int64, max int) (string, error)
}

// Tools is the set the model is offered, over one View.
type Tools struct{ view View }

// NewTools returns the tools over that view of what Owl knows.
func NewTools(view View) *Tools { return &Tools{view: view} }

// Definitions is what the provider is told the model may ask for.
func (t *Tools) Definitions() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        toolListProjects,
			Description: "List the Projects Owl knows: their names, paths and base branches.",
			Schema:      object(nil, nil),
		},
		{
			Name: toolListJobs,
			Description: "List Jobs. Without `all` this is the queue - the Jobs waiting to run, " +
				"in the order they will run. With `all` it is every Job whatever its state.",
			Schema: object(map[string]any{
				"all": map[string]any{
					"type":        "boolean",
					"description": "List every Job rather than only the queue.",
				},
			}, nil),
		},
		{
			Name: toolGetJob,
			Description: "Everything Owl knows about one Job: its state and why it is in it, its " +
				"Runs, what each Verification check said and what it printed, its plan and its handoff.",
			Schema: object(map[string]any{
				"id": map[string]any{"type": "integer", "description": "The Job's id."},
			}, []string{"id"}),
		},
		{
			Name: toolReadRunLog,
			Description: "The end of one Run's captured output, as the Agent wrote it. Use it when " +
				"a Job's own report does not say enough about what the Agent did.",
			Schema: object(map[string]any{
				"run_id": map[string]any{"type": "integer", "description": "The Run's id."},
			}, []string{"run_id"}),
		},
		{
			Name:        toolGetJobDiff,
			Description: "What a Job's branch changed against its Project's base branch, as a patch.",
			Schema: object(map[string]any{
				"id": map[string]any{"type": "integer", "description": "The Job's id."},
			}, []string{"id"}),
		},
	}
}

// Call carries out one tool call and returns what to tell the model. A call
// Owl cannot answer is answered with why, rather than ending the conversation:
// the model asked for something, and being told no is an answer it can use.
func (t *Tools) Call(ctx context.Context, call ToolCall) ToolResult {
	out := ToolResult{CallID: call.ID}
	text, err := t.answer(ctx, call)
	if err != nil {
		// Why it could not be done is bounded too: it carries what a Project,
		// a Job or a provider said, which is not Owl's text.
		out.Text, out.Failed = cut(err.Error(), maxResult), true
		return out
	}
	out.Text = cut(text, maxResult)
	return out
}

// answer is what one tool says.
func (t *Tools) answer(ctx context.Context, call ToolCall) (string, error) {
	switch call.Name {
	case toolListProjects:
		return t.view.Projects(ctx)
	case toolListJobs:
		var args struct {
			All bool `json:"all"`
		}
		if err := arguments(call, &args); err != nil {
			return "", err
		}
		return t.view.Jobs(ctx, args.All)
	case toolGetJob:
		id, err := idArgument(call, "id")
		if err != nil {
			return "", err
		}
		return t.view.Job(ctx, id)
	case toolReadRunLog:
		id, err := idArgument(call, "run_id")
		if err != nil {
			return "", err
		}
		return t.view.RunLog(ctx, id, maxResult)
	case toolGetJobDiff:
		id, err := idArgument(call, "id")
		if err != nil {
			return "", err
		}
		return t.view.JobDiff(ctx, id, maxResult)
	default:
		// The set is closed, and a name outside it is a model asking for
		// something Owl does not offer - including anything that would act.
		return "", fmt.Errorf("%q is not a tool Owl has; it has %s",
			call.Name, strings.Join(t.names(), ", "))
	}
}

// names is what the tools are called, for the message a refusal carries.
func (t *Tools) names() []string {
	defs := t.Definitions()
	out := make([]string, 0, len(defs))
	for _, d := range defs {
		out = append(out, d.Name)
	}
	return out
}

// arguments reads a call's arguments into v.
func arguments(call ToolCall, v any) error {
	raw := strings.TrimSpace(string(call.Input))
	if raw == "" || raw == "null" {
		return nil
	}
	if err := json.Unmarshal([]byte(raw), v); err != nil {
		return fmt.Errorf("the arguments for %s are not something Owl can read: %v", call.Name, err)
	}
	return nil
}

// idArgument is one id argument, which a model may write as a number or as a
// string of one.
func idArgument(call ToolCall, name string) (int64, error) {
	var args map[string]json.RawMessage
	if err := arguments(call, &args); err != nil {
		return 0, err
	}
	raw, ok := args[name]
	if !ok {
		return 0, fmt.Errorf("%s needs %s", call.Name, name)
	}
	var id int64
	if err := json.Unmarshal(raw, &id); err == nil {
		return id, nil
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		if err := json.Unmarshal([]byte(strings.TrimSpace(text)), &id); err == nil {
			return id, nil
		}
	}
	return 0, fmt.Errorf("%s is not an id: %s", name, raw)
}

// object is a JSON schema for a tool's arguments.
func object(properties map[string]any, required []string) map[string]any {
	if properties == nil {
		properties = map[string]any{}
	}
	out := map[string]any{"type": "object", "properties": properties}
	if len(required) > 0 {
		out["required"] = required
	}
	return out
}

// cut is text as much of as fits, ending at a whole line where there is one
// and saying that the rest is not there.
func cut(text string, max int) string {
	if len(text) <= max {
		return text
	}
	kept := text[:max]
	if at := strings.LastIndexByte(kept, '\n'); at >= 0 {
		kept = kept[:at+1]
	}
	return kept + "\n[the rest is not carried: this is the first " +
		fmt.Sprint(len(kept)) + " bytes of " + fmt.Sprint(len(text)) + "]"
}
