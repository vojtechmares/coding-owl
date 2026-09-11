package idle_test

import (
	"strings"
	"testing"
	"time"

	"github.com/vojtechmares/coding-owl/internal/idle"
)

func TestThePolicyIsWhatMakesAReadingIdle(t *testing.T) {
	for what, c := range map[string]struct {
		policy idle.Policy
		state  idle.State
		want   bool
		says   string
	}{
		"long enough on power": {
			policy: idle.DefaultPolicy(),
			state:  idle.State{Since: 11 * time.Minute, OnPower: true},
			want:   true,
		},
		"long enough on battery": {
			policy: idle.DefaultPolicy(),
			state:  idle.State{Since: time.Hour, OnPower: false},
			says:   "battery",
		},
		"on power but only just left": {
			policy: idle.DefaultPolicy(),
			state:  idle.State{Since: 9*time.Minute + 59*time.Second, OnPower: true},
			says:   "in use",
		},
		"on battery when the policy does not mind": {
			policy: idle.Policy{After: time.Minute},
			state:  idle.State{Since: 2 * time.Minute, OnPower: false},
			want:   true,
		},
		"a policy that asks for longer": {
			policy: idle.Policy{After: time.Hour, RequirePower: true},
			state:  idle.State{Since: 30 * time.Minute, OnPower: true},
			says:   "in use",
		},
	} {
		got, why := c.policy.Allows(c.state)

		if got != c.want {
			t.Errorf("%s: Allows = %v, want %v", what, got, c.want)
		}
		if c.want && why != "" {
			t.Errorf("%s: an idle machine was given the reason %q", what, why)
		}
		if !c.want && !strings.Contains(why, c.says) {
			t.Errorf("%s: Allows says %q, want it to say %q", what, why, c.says)
		}
	}
}
