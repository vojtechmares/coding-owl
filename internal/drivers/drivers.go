// Package drivers is every Driver Owl has, by name. It is the one place that
// knows which coding tools exist, so that the command tree and the daemon
// agree on the list without either of them naming a tool's own flags
// (ADR-0018).
package drivers

import (
	"fmt"
	"sort"
	"strings"

	"github.com/vojtechmares/coding-owl/internal/config"
	"github.com/vojtechmares/coding-owl/internal/driver"
	"github.com/vojtechmares/coding-owl/internal/driver/claudecode"
)

// Default is the Driver an Account belongs to when nobody says otherwise.
const Default = "claude-code"

// all is every Driver Owl has, by name, each built from the daemon's own
// configuration: what a Driver needs to know about the machine - where its
// tool is - lives there.
var all = map[string]func(config.Global) driver.Driver{
	Default: func(g config.Global) driver.Driver { return claudecode.NewWithPath(g.ClaudePath) },
}

// Known reports whether Owl has a Driver of that name.
func Known(name string) bool {
	_, ok := all[name]
	return ok
}

// Lookup returns the Driver of that name, built from the daemon's own
// configuration.
func Lookup(name string, global config.Global) (driver.Driver, bool) {
	make, ok := all[name]
	if !ok {
		return nil, false
	}
	return make(global), true
}

// InstructionsFile is the file a Driver's tool reads an Account's standing
// instructions from, and empty for a name that is not a Driver or a tool that
// reads none (ADR-0037).
//
// The Driver is built from an empty configuration because the answer is the
// tool's own constant: where its binary is on this machine does not change
// what it calls that file. Anything that has to run the tool asks Lookup for
// a Driver built from the daemon's real configuration instead.
func InstructionsFile(name string) string {
	d, ok := Lookup(name, config.Global{})
	if !ok {
		return ""
	}
	return d.InstructionsFile()
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
