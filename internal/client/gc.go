package client

import (
	"context"
	"fmt"
	"time"

	"connectrpc.com/connect"

	codingowlv1 "github.com/vojtechmares/coding-owl/gen/codingowl/v1"
)

// Unfinished is one piece of work garbage collection would not touch, as the
// daemon reports it (ADR-0015).
type Unfinished struct {
	// Job is the Job it belongs to, zero for a worktree belonging to none.
	Job int64
	// Project is the Project it belongs to, empty when nothing says.
	Project string
	// Path is the worktree it is about, empty when it is about a Job rather
	// than a directory.
	Path string
	// Reason says what is unfinished about it, in the words a person reads.
	Reason string
	// Since is how long it has been that way, zero for what has no clock on
	// it.
	Since time.Duration
}

// Describe renders one piece of unfinished work as a sentence.
func (u Unfinished) Describe() string {
	what := u.Path
	if what == "" {
		what = fmt.Sprintf("job %d", u.Job)
	}
	line := what + " " + u.Reason
	if u.Since > 0 {
		line += " for " + humanDuration(u.Since)
	}
	return line
}

// humanDuration renders how long something has been waiting the way a person
// says it, rather than as a duration with three units in it.
func humanDuration(d time.Duration) string {
	switch {
	case d >= 48*time.Hour:
		return fmt.Sprintf("%d days", int(d.Hours()/24))
	case d >= 2*time.Hour:
		return fmt.Sprintf("%d hours", int(d.Hours()))
	case d >= 2*time.Minute:
		return fmt.Sprintf("%d minutes", int(d.Minutes()))
	default:
		return "less than a minute"
	}
}

// Reclaimed is one worktree garbage collection took back.
type Reclaimed struct {
	Job  int64
	Path string
	Why  string
}

// Accepted is one Job garbage collection finished because its work is already
// in its Project's base branch.
type Accepted struct {
	Job    int64
	Branch string
	Base   string
}

// Collection is what one garbage collection did.
type Collection struct {
	Reclaimed  []Reclaimed
	Accepted   []Accepted
	Pruned     []string
	Unfinished []Unfinished
}

// Empty reports whether the collection had nothing at all to say.
func (c Collection) Empty() bool {
	return len(c.Reclaimed) == 0 && len(c.Accepted) == 0 &&
		len(c.Pruned) == 0 && len(c.Unfinished) == 0
}

// Collect runs one garbage collection and reports what it did.
func (c *Client) Collect(ctx context.Context) (Collection, error) {
	res, err := c.gc.Collect(ctx, connect.NewRequest(&codingowlv1.CollectRequest{}))
	if err != nil {
		return Collection{}, c.wrap(err)
	}
	out := Collection{Pruned: res.Msg.GetPruned()}
	for _, r := range res.Msg.GetReclaimed() {
		out.Reclaimed = append(out.Reclaimed, Reclaimed{
			Job: r.GetJobId(), Path: r.GetPath(), Why: r.GetWhy(),
		})
	}
	for _, a := range res.Msg.GetAccepted() {
		out.Accepted = append(out.Accepted, Accepted{
			Job: a.GetJobId(), Branch: a.GetBranch(), Base: a.GetBase(),
		})
	}
	out.Unfinished = unfinishedFromProto(res.Msg.GetUnfinished())
	return out, nil
}

// unfinishedReasons translate the wire's reason back into the words a person
// reads, which is what the CLI and the desktop app show.
var unfinishedReasons = map[codingowlv1.UnfinishedReason]string{
	codingowlv1.UnfinishedReason_UNFINISHED_REASON_UNCOMMITTED: "holds changes nobody has committed",
	codingowlv1.UnfinishedReason_UNFINISHED_REASON_WAITING:     "has been waiting for a decision",
	codingowlv1.UnfinishedReason_UNFINISHED_REASON_ABANDONED:   "is active but no daemon is running it",
}

// unknownReason is shown for a reason this client does not know, which is what
// an older client sees when the daemon has learned a new one.
const unknownReason = "is unfinished for a reason this owl does not know"

func unfinishedFromProto(rows []*codingowlv1.Unfinished) []Unfinished {
	out := make([]Unfinished, 0, len(rows))
	for _, u := range rows {
		reason, ok := unfinishedReasons[u.GetReason()]
		if !ok {
			reason = unknownReason
		}
		out = append(out, Unfinished{
			Job:     u.GetJobId(),
			Project: u.GetProject(),
			Path:    u.GetPath(),
			Reason:  reason,
			Since:   u.GetSince().AsDuration(),
		})
	}
	return out
}
