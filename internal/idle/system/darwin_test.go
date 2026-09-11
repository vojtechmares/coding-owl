//go:build darwin

package system_test

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/vojtechmares/coding-owl/internal/idle"
	"github.com/vojtechmares/coding-owl/internal/idle/system"
)

var ctx = context.Background()

// machine puts tools that print those lines where the system's own would be
// found, and returns the Detector that reads them. A body beginning with `!`
// is a tool that fails, saying the rest on the way out.
func machine(t *testing.T, ioreg, pmset string) idle.Detector {
	t.Helper()
	dir := t.TempDir()
	for name, body := range map[string]string{"ioreg": ioreg, "pmset": pmset} {
		script := "#!/bin/sh\n"
		if said, failed := strings.CutPrefix(body, "!"); failed {
			script += "echo " + shellQuote(said) + " >&2\nexit 1\n"
		} else {
			script += "cat <<'OUTPUT'\n" + body + "\nOUTPUT\n"
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	// First, rather than only: the script itself needs the shell's own tools.
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return system.New()
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// ioregSaying is what ioreg prints for a machine untouched for that long.
func ioregSaying(d time.Duration) string {
	return `+-o IOHIDSystem  <class IOHIDSystem, id 0x100000282, registered>
    {
      "IOClass" = "IOHIDSystem"
      "HIDIdleTime" = ` + strconv.FormatInt(int64(d), 10) + `
    }`
}

const onAC = "Now drawing from 'AC Power'\n -InternalBattery-0 (id=1)\t100%; charged; 0:00 remaining present: true"

const onBattery = "Now drawing from 'Battery Power'\n -InternalBattery-0 (id=1)\t80%; discharging; 3:00 remaining"

func TestTheMachineIsReadAsTheSystemReportsIt(t *testing.T) {
	d := machine(t, ioregSaying(725*time.Second), onAC)

	got, err := d.Read(ctx)

	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got.Since != 725*time.Second {
		t.Errorf("the machine has been idle %v, want what the system said", got.Since)
	}
	if !got.OnPower {
		t.Error("the machine is reported as on battery though the system says AC power")
	}
}

func TestAMachineOnBatteryIsReadAsOne(t *testing.T) {
	d := machine(t, ioregSaying(time.Hour), onBattery)

	got, err := d.Read(ctx)

	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got.OnPower {
		t.Error("the machine is reported as on AC power though the system says battery")
	}
	if got.Since != time.Hour {
		t.Errorf("the machine has been idle %v, want an hour", got.Since)
	}
}

func TestTheMostRecentInputIsWhatCounts(t *testing.T) {
	// Two entries: one device untouched for an hour, another touched a second
	// ago. Somebody is at the machine.
	both := ioregSaying(time.Hour) + "\n" + ioregSaying(time.Second)
	d := machine(t, both, onAC)

	got, err := d.Read(ctx)

	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got.Since != time.Second {
		t.Errorf("the machine has been idle %v, want the most recent input across the entries", got.Since)
	}
}

func TestAMachineThatCannotBeReadIsAFailureRatherThanAnIdleOne(t *testing.T) {
	for what, c := range map[string]struct{ ioreg, pmset, says string }{
		"nothing answers about the input": {ioreg: "!ioreg: no such class", pmset: onAC, says: "ioreg"},
		"nothing answers about the power": {ioreg: ioregSaying(time.Minute), pmset: "!pmset: cannot", says: "pmset"},
		"the input is not in the answer":  {ioreg: "+-o IOHIDSystem\n{\n}", pmset: onAC, says: "idle"},
		"the power is not in the answer":  {ioreg: ioregSaying(time.Minute), pmset: "nothing useful", says: "drawing"},
		"the input is not one Owl can hold": {
			ioreg: `"HIDIdleTime" = 99999999999999999999999`, pmset: onAC, says: "idle",
		},
		"the input is not a number at all": {ioreg: `"HIDIdleTime" = <pointer>`, pmset: onAC, says: "idle"},
	} {
		d := machine(t, c.ioreg, c.pmset)

		got, err := d.Read(ctx)

		if err == nil {
			t.Errorf("%s: Read = %+v, want the reading refused", what, got)
			continue
		}
		if !strings.Contains(err.Error(), c.says) {
			t.Errorf("%s: Read = %v, want it to say %q", what, err, c.says)
		}
	}
}
