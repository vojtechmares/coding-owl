// Command fakemachine stands where the system's own `ioreg` and `pmset` do,
// first on the daemon's PATH, so a scenario can step away from the machine and
// come back to it (issue #19). Which one it is standing in for is decided by
// the name it was invoked as, the way the system's own tools are told apart.
//
// What the machine is doing is read from the file named by OWL_FAKE_MACHINE on
// every invocation, so a scenario changes it by writing that file: one line of
// `<idle seconds> <ac|battery>`, or `unreadable` for a machine nothing can
// read.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func main() {
	state, err := read()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	switch name := filepath.Base(os.Args[0]); name {
	case "ioreg":
		ioreg(state)
	case "pmset":
		pmset(state)
	default:
		fmt.Fprintf(os.Stderr, "fakemachine: nothing is called %s\n", name)
		os.Exit(1)
	}
}

// state is what the machine is doing.
type state struct {
	idle    int
	onPower bool
}

// read is the machine as the file says it is. A file that says `unreadable`
// stands for a machine whose state nothing can get at, which is a failure
// rather than an answer.
func read() (state, error) {
	path := os.Getenv("OWL_FAKE_MACHINE")
	if path == "" {
		return state{}, fmt.Errorf("fakemachine: OWL_FAKE_MACHINE names no file")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return state{}, fmt.Errorf("fakemachine: %w", err)
	}
	said := strings.Fields(string(raw))
	if len(said) == 1 && said[0] == "unreadable" {
		return state{}, fmt.Errorf("fakemachine: this machine cannot be read")
	}
	if len(said) != 2 {
		return state{}, fmt.Errorf("fakemachine: %q is not `<seconds> <ac|battery>`", strings.TrimSpace(string(raw)))
	}
	idle, err := strconv.Atoi(said[0])
	if err != nil {
		return state{}, fmt.Errorf("fakemachine: %q is not a number of seconds", said[0])
	}
	return state{idle: idle, onPower: said[1] == "ac"}, nil
}

// ioreg prints what `ioreg -c IOHIDSystem -d 4 -r` prints, in the shape the
// idle time is read out of: HIDIdleTime is nanoseconds since the last input.
func ioreg(s state) {
	fmt.Printf(`+-o IOHIDSystem  <class IOHIDSystem, id 0x100000282, registered, matched, active, busy 0 (0 ms), retain 8>
    {
      "IOClass" = "IOHIDSystem"
      "IOProviderClass" = "IOResources"
      "HIDIdleTime" = %d
      "HIDPointerAcceleration" = 45056
    }

`, int64(s.idle)*1_000_000_000)
}

// pmset prints what `pmset -g ps` prints, whose first line says what the
// machine is drawing from.
func pmset(s state) {
	drawing := "Battery Power"
	if s.onPower {
		drawing = "AC Power"
	}
	fmt.Printf("Now drawing from '%s'\n", drawing)
	fmt.Printf(" -InternalBattery-0 (id=1234567)\t100%%; charged; 0:00 remaining present: true\n")
}
