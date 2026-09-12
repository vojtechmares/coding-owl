package run

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/vojtechmares/coding-owl/internal/config"
	"github.com/vojtechmares/coding-owl/internal/driver"
	"github.com/vojtechmares/coding-owl/internal/store"
)

// The windows Owl holds an Account to, in its own words: what a tool calls
// them is the tool's business (ADR-0020).
const (
	// WindowFiveHour is the short window.
	WindowFiveHour = "five-hour"
	// WindowWeekly is the seven-day window, which a tool may report per tier.
	// The most utilized of them is what a weekly ceiling is kept against,
	// which is conservative and wrong in nobody's favour (ADR-0020).
	WindowWeekly = "weekly"
)

// fiveHourName is what the tool calls the short window, and weeklyPrefix what
// its seven-day windows all begin with.
const (
	fiveHourName = "five_hour"
	weeklyPrefix = "seven_day"
)

// AccountCeiling is what one Account is at against what it is held to, as
// `owl status` and the app report it.
type AccountCeiling struct {
	// Name is the Account.
	Name string
	// Windows is what is known about each of its windows, and what each is
	// held to. Empty when nothing has been read about it yet.
	Windows []WindowUsage
	// Waiting is why work on this Account is being held back, empty when
	// nothing is holding it back.
	Waiting string
	// Until is when the window that holds it back starts again.
	Until time.Time
}

// WindowUsage is one window: how much of it is spent, and how much of it Owl
// will work into.
type WindowUsage struct {
	// Name is the window in Owl's words.
	Name string
	// Utilization is how much of it is spent, as a percentage. It is
	// account-wide: it counts what the user spent themselves.
	Utilization float64
	// Ceiling is how much of it Owl will work into, zero when nobody set one.
	Ceiling float64
	// Resets is when the window starts again.
	Resets time.Time
}

// over reports whether this window is one Owl will not work into.
func (w WindowUsage) over() bool { return w.Ceiling > 0 && w.Utilization >= w.Ceiling }

// recordUsage writes down what a Run's stream said about the Account it ran
// on. Nothing is held against a Run that says nothing: most lines do.
func (s *Service) recordUsage(ctx context.Context, account string, u driver.Usage) {
	if strings.TrimSpace(account) == "" {
		return
	}
	now := s.now().UTC()
	for _, w := range u.Windows {
		// A window that has already started again is not one this figure is
		// about. A tool reporting one is a tool a moment behind the clock, and
		// keeping it would hold work back for a reason that has passed.
		if !w.Resets.After(now) {
			continue
		}
		if err := s.opts.Store.RecordAccountUsage(ctx, store.AccountUsage{
			Account: account, Window: w.Name, Utilization: w.Utilization,
			Resets: w.Resets, Observed: now,
		}); err != nil {
			s.opts.Logger.Error("recording what a run reported about an account",
				"account", account, "window", w.Name, "error", err)
		}
	}
}

// watchUsage reads what one line of a Run's stream said about the Account it
// draws on, writes it down, and ends the Run when it has taken that Account
// past its ceiling (ADR-0020). Most lines say nothing, and nothing is held
// against a Run for that.
//
// The writing outlives the Run's own context: a figure read from a Run that is
// being stopped is still what the account is at.
func (s *Service) watchUsage(r store.Run, account, line string) {
	u, ok := s.opts.Driver.Usage(line)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(s.ctx), bookkeepingTimeout)
	defer cancel()
	s.recordUsage(ctx, account, u)
	if why, over := s.crossedCeiling(ctx, account); over {
		s.opts.Logger.Info("a run took its account past its ceiling", "run", r.ID, "reason", why)
		s.end(r.ID, why)
	}
}

// Ceilings is every Account against what it is held to, with the readings
// whose windows have started again dropped first: a figure about a window that
// is over says nothing about the one that replaced it (ADR-0020).
func (s *Service) Ceilings(ctx context.Context) ([]AccountCeiling, error) {
	if err := s.forgetResetWindows(ctx); err != nil {
		return nil, err
	}
	accounts, err := s.opts.Accounts.List(ctx)
	if err != nil {
		return nil, err
	}
	readings, err := s.opts.Store.ListAccountUsage(ctx)
	if err != nil {
		return nil, err
	}
	global, err := s.global()
	if err != nil {
		return nil, err
	}
	out := make([]AccountCeiling, 0, len(accounts))
	for _, a := range accounts {
		limits, _ := global.Ceiling(a.Name)
		c := AccountCeiling{Name: a.Name, Windows: windowsOf(a.Name, readings, limits)}
		if w, ok := heldBack(c.Windows); ok {
			c.Waiting, c.Until = aboveCeiling(a.Name, w), w.Resets
		}
		out = append(out, c)
	}
	return out, nil
}

// windowsOf folds what was read about one Account into the windows Owl holds
// it to: the short one as the tool reported it, and the weekly one as the most
// utilized of however many the tool splits it into.
func windowsOf(account string, readings []store.AccountUsage, limits config.Limits) []WindowUsage {
	var out []WindowUsage
	var weekly *WindowUsage
	for _, r := range readings {
		if !strings.EqualFold(r.Account, account) {
			continue
		}
		switch {
		case r.Window == fiveHourName:
			out = append(out, WindowUsage{
				Name: WindowFiveHour, Utilization: r.Utilization,
				Ceiling: limits.FiveHourMax, Resets: r.Resets,
			})
		case strings.HasPrefix(r.Window, weeklyPrefix):
			if weekly == nil || r.Utilization > weekly.Utilization {
				weekly = &WindowUsage{
					Name: WindowWeekly, Utilization: r.Utilization,
					Ceiling: limits.WeeklyMax, Resets: r.Resets,
				}
			}
		}
	}
	if weekly != nil {
		out = append(out, *weekly)
	}
	return out
}

// heldBack is the window that stops work on an Account, if one does.
func heldBack(windows []WindowUsage) (WindowUsage, bool) {
	for _, w := range windows {
		if w.over() {
			return w, true
		}
	}
	return WindowUsage{}, false
}

// aboveCeiling is what a person is told about an Account that is over: what it
// is at, what it is held to, and when that stops being true.
func aboveCeiling(account string, w WindowUsage) string {
	return fmt.Sprintf("account %s is at %s of its %s window, above the %s ceiling; it resets at %s",
		account, percent(w.Utilization), w.Name, percent(w.Ceiling), w.Resets.Format(time.RFC3339))
}

// percent is a share of a window as a person reads it.
func percent(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) + "%" }

// underCeiling reports whether a Run may start on that Account. Being over is
// a refusal rather than a failure: nothing is wrong with the work, and the Job
// waits exactly where it is until the window starts again (ADR-0020).
func (s *Service) underCeiling(ctx context.Context, account string) error {
	global, err := s.global()
	if err != nil {
		return err
	}
	limits, ok := global.Ceiling(account)
	if !ok {
		return nil
	}
	// A ceiling is kept against what the tool reports. One that reports
	// nothing cannot be held to it, and Owl says so rather than working on in
	// the dark (ADR-0020).
	if !s.opts.Driver.Capabilities().UsageReporting {
		return refused(
			"account %s is held to a ceiling, and %s does not report what an account has used; "+
				"a ceiling cannot be kept without it",
			account, s.opts.Driver.Name())
	}
	if err := s.forgetResetWindows(ctx); err != nil {
		return err
	}
	readings, err := s.opts.Store.ListAccountUsage(ctx)
	if err != nil {
		return err
	}
	if w, ok := heldBack(windowsOf(account, readings, limits)); ok {
		return refused("%s", aboveCeiling(account, w))
	}
	return nil
}

// crossedCeiling is the window a Run has just taken an Account past, if it has.
// It reads what was just recorded rather than the reading alone, so that a Run
// which crosses a window it did not report anything about is caught too.
func (s *Service) crossedCeiling(ctx context.Context, account string) (string, bool) {
	global, err := s.global()
	if err != nil {
		s.opts.Logger.Error("reading what an account is held to", "account", account, "error", err)
		return "", false
	}
	limits, ok := global.Ceiling(account)
	if !ok {
		return "", false
	}
	// A window that started again while the Run was going is not one to end it
	// for, so what is judged is what is still about this window.
	if err := s.forgetResetWindows(ctx); err != nil {
		s.opts.Logger.Error("forgetting what is no longer about this window", "error", err)
		return "", false
	}
	readings, err := s.opts.Store.ListAccountUsage(ctx)
	if err != nil {
		s.opts.Logger.Error("reading what an account has used", "account", account, "error", err)
		return "", false
	}
	w, over := heldBack(windowsOf(account, readings, limits))
	if !over {
		return "", false
	}
	return aboveCeiling(account, w), true
}

// forgetResetWindows drops the readings whose windows have started again.
func (s *Service) forgetResetWindows(ctx context.Context) error {
	n, err := s.opts.Store.ForgetResetWindows(ctx, s.now().UTC())
	if err != nil {
		return err
	}
	if n > 0 {
		s.opts.Logger.Debug("readings whose windows have started again were forgotten", "readings", n)
	}
	return nil
}
