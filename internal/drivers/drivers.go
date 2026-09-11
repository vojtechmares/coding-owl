// Package drivers is every Driver Owl has, by name. It is the one place that
// knows which coding tools exist, so that the command tree and the daemon
// agree on the list without either of them naming a tool's own flags
// (ADR-0018).
package drivers

import (
	"fmt"
	"sort"
	"strings"

	"github.com/vojtechmares/coding-owl/internal/driver"
	"github.com/vojtechmares/coding-owl/internal/driver/claudecode"
)

// Default is the Driver an Account belongs to when nobody says otherwise.
const Default = "claude-code"

// all is every Driver Owl has, by name.
var all = map[string]func() driver.Driver{
	Default: func() driver.Driver { return claudecode.New() },
}

// Lookup returns the Driver of that name.
func Lookup(name string) (driver.Driver, bool) {
	make, ok := all[name]
	if !ok {
		return nil, false
	}
	return make(), true
}

// Names is every Driver Owl has, in order.
func Names() []string {
	out := make([]string, 0, len(all))
	for name := range all {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// Unknown is the error for a name that is not a Driver, naming the ones that
// are: a user who mistyped one wants the list, not a refusal.
func Unknown(name string) error {
	return fmt.Errorf("%q is not a driver Owl has; it has %s", name, strings.Join(Names(), ", "))
}
