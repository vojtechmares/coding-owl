package store

// That a Job's labels go when the Job does (issue #132). It is an internal
// test because what it asks about is a row nobody owns any more, and there is
// no Job left to read it off.

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestDeletingAJobTakesItsLabelsWithIt(t *testing.T) {
	ctx := context.Background()
	s, _, err := Open(filepath.Join(t.TempDir(), "owl.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if err := s.AddProject(ctx, Project{
		Name: "api", Path: "/repos/api", BaseBranch: "main", Registered: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("AddProject: %v", err)
	}
	if _, err := s.UpsertJob(ctx, Job{
		Source: "local", SourceRef: "a", Project: "api", Prompt: "first",
		State: "pending", Labels: []string{"bug"}, Created: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("UpsertJob: %v", err)
	}

	// Removing the Project takes its Jobs, and each Job must take its labels:
	// a row left behind would be waiting for whichever Job is next given that
	// id, and the user would be shown a label they never wrote.
	if _, err := s.RemoveProject(ctx, "api"); err != nil {
		t.Fatalf("RemoveProject: %v", err)
	}

	var left int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM job_labels`).Scan(&left); err != nil {
		t.Fatalf("counting job_labels: %v", err)
	}
	if left != 0 {
		t.Errorf("%d label rows outlived the job they belonged to, want none", left)
	}
}
