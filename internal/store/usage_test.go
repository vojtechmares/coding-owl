package store_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/vojtechmares/coding-owl/internal/store"
)

// reading is a figure about one window, for the tests below.
func reading(account, window string, pct float64, resets time.Time) store.AccountUsage {
	return store.AccountUsage{
		Account: account, Window: window, Utilization: pct,
		Resets: resets, Observed: time.Now().UTC(),
	}
}

func TestAccountUsageKeepsOnlyTheLastReadingOfAWindow(t *testing.T) {
	s := openStore(t, filepath.Join(t.TempDir(), "owl.db"))
	resets := time.Now().Add(time.Hour).UTC().Truncate(time.Second)

	if err := s.RecordAccountUsage(ctx, reading("work", "five_hour", 10, resets)); err != nil {
		t.Fatalf("RecordAccountUsage: %v", err)
	}
	if err := s.RecordAccountUsage(ctx, reading("work", "five_hour", 42, resets)); err != nil {
		t.Fatalf("RecordAccountUsage: %v", err)
	}
	if err := s.RecordAccountUsage(ctx, reading("work", "seven_day", 7, resets)); err != nil {
		t.Fatalf("RecordAccountUsage: %v", err)
	}

	got, err := s.ListAccountUsage(ctx)
	if err != nil {
		t.Fatalf("ListAccountUsage: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("ListAccountUsage = %+v, want one row per window", got)
	}
	if got[0].Window != "five_hour" || got[0].Utilization != 42 {
		t.Errorf("the five-hour row is %+v, want the reading that replaced the first", got[0])
	}
	if !got[0].Resets.Equal(resets) {
		t.Errorf("the row resets at %s, want %s", got[0].Resets, resets)
	}
}

func TestAccountUsageForgetsWindowsThatHaveStartedAgain(t *testing.T) {
	s := openStore(t, filepath.Join(t.TempDir(), "owl.db"))
	now := time.Now().UTC()
	if err := s.RecordAccountUsage(ctx, reading("work", "five_hour", 90, now.Add(-time.Minute))); err != nil {
		t.Fatalf("RecordAccountUsage: %v", err)
	}
	if err := s.RecordAccountUsage(ctx, reading("work", "seven_day", 20, now.Add(time.Hour))); err != nil {
		t.Fatalf("RecordAccountUsage: %v", err)
	}

	n, err := s.ForgetResetWindows(ctx, now)

	if err != nil {
		t.Fatalf("ForgetResetWindows: %v", err)
	}
	if n != 1 {
		t.Errorf("ForgetResetWindows dropped %d rows, want the one whose window is over", n)
	}
	got, err := s.ListAccountUsage(ctx)
	if err != nil {
		t.Fatalf("ListAccountUsage: %v", err)
	}
	if len(got) != 1 || got[0].Window != "seven_day" {
		t.Errorf("ListAccountUsage = %+v, want only the window that has not started again", got)
	}
}

func TestAccountUsageIsForgottenWithItsAccount(t *testing.T) {
	s := openStore(t, filepath.Join(t.TempDir(), "owl.db"))
	resets := time.Now().Add(time.Hour).UTC()
	for _, a := range []string{"work", "spare"} {
		if err := s.RecordAccountUsage(ctx, reading(a, "five_hour", 10, resets)); err != nil {
			t.Fatalf("RecordAccountUsage: %v", err)
		}
	}

	if err := s.ForgetAccountUsage(ctx, "work"); err != nil {
		t.Fatalf("ForgetAccountUsage: %v", err)
	}

	got, err := s.ListAccountUsage(ctx)
	if err != nil {
		t.Fatalf("ListAccountUsage: %v", err)
	}
	if len(got) != 1 || got[0].Account != "spare" {
		t.Errorf("ListAccountUsage = %+v, want only the account that is still there", got)
	}
}

func TestAccountUsageIsBoundedByWhatOwlKeeps(t *testing.T) {
	// Not a store rule but the one the store would otherwise carry: see
	// internal/run, which is what decides how many windows an Account keeps.
	s := openStore(t, filepath.Join(t.TempDir(), "owl.db"))
	resets := time.Now().Add(time.Hour).UTC()
	for at := range 3 {
		if err := s.RecordAccountUsage(ctx, reading("work", "seven_day_"+string(rune('a'+at)), 10, resets)); err != nil {
			t.Fatalf("RecordAccountUsage: %v", err)
		}
	}

	got, err := s.ListAccountUsage(ctx)

	if err != nil {
		t.Fatalf("ListAccountUsage: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("ListAccountUsage = %+v, want a row per window", got)
	}
	// In a steady order, so a report reads the same twice.
	if got[0].Window != "seven_day_a" || got[2].Window != "seven_day_c" {
		t.Errorf("ListAccountUsage = %+v, want the windows in order", got)
	}
}
