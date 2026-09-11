package client_test

import (
	"strings"
	"testing"
	"time"

	"github.com/vojtechmares/coding-owl/internal/client"
)

func TestDescribeSaysWhatIsUnfinishedAboutIt(t *testing.T) {
	for _, c := range []struct {
		what client.Unfinished
		want []string
	}{
		{
			what: client.Unfinished{Path: "/worktrees/1", Reason: "holds changes nobody has committed"},
			want: []string{"/worktrees/1", "committed"},
		},
		{
			what: client.Unfinished{Job: 7, Reason: "has been waiting for a decision", Since: 3 * 24 * time.Hour},
			want: []string{"job 7", "decision", "3 days"},
		},
		{
			what: client.Unfinished{Job: 9, Reason: "is active but no daemon is running it", Since: time.Second},
			want: []string{"job 9", "running", "less than a minute"},
		},
		{
			what: client.Unfinished{Job: 2, Reason: "has been waiting for a decision", Since: 5 * time.Hour},
			want: []string{"job 2", "5 hours"},
		},
		{
			what: client.Unfinished{Job: 3, Reason: "has been waiting for a decision", Since: 7 * time.Minute},
			want: []string{"job 3", "7 minutes"},
		},
	} {
		got := c.what.Describe()
		for _, want := range c.want {
			if !strings.Contains(got, want) {
				t.Errorf("Describe() = %q, want it to say %q", got, want)
			}
		}
	}
}

func TestDescribeLeavesOutAClockThereIsNoneFor(t *testing.T) {
	got := client.Unfinished{Path: "/worktrees/1", Reason: "holds changes nobody has committed"}.Describe()

	if strings.Contains(got, "for ") {
		t.Errorf("Describe() = %q, want no length of time for work that has none", got)
	}
}
