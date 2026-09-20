package cli

// A person is not a wedged daemon.
//
// withDaemon puts a deadline on a call so that a daemon which has stopped
// answering cannot hold the command forever. Anything that reads from the
// terminal inside that deadline puts the user's own reading and typing on the
// same clock, and they lose their answers to a timeout they had no way to see
// coming - which is exactly what `owl project setup` did until it asked
// between two calls rather than inside one.
//
// A test that proved it by waiting out a real deadline would add half a minute
// of sleeping to every run. This reads the package instead and asks the
// question structurally, which is what makes it free.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// bounded are the helpers that put a deadline on what they run.
var bounded = map[string]bool{"withDaemon": true, "withTimeout": true}

// asksAPerson are the calls that wait for someone to read something and type
// an answer. Reading a whole stream that is already there - a pasted token -
// is not waiting on a person in the same way, so only the prompting reader is
// named here.
var asksAPerson = map[string]bool{"choose": true, "pickAccount": true, "pickLocation": true}

func TestNothingAsksAPersonInsideADaemonDeadline(t *testing.T) {
	set := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("reading the package: %v", err)
	}
	read := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(set, filepath.Join(".", name), nil, 0)
		if err != nil {
			t.Fatalf("reading %s: %v", name, err)
		}
		read++
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || !bounded[calledName(call.Fun)] {
				return true
			}
			// Everything the bounded call runs, which is whatever was handed
			// to it.
			for _, arg := range call.Args {
				ast.Inspect(arg, func(inner ast.Node) bool {
					asked, ok := inner.(*ast.CallExpr)
					if !ok {
						return true
					}
					if who := calledName(asked.Fun); asksAPerson[who] {
						t.Errorf("%s: %s asks a person inside %s, so the call deadline runs "+
							"while they read and type; ask between bounded calls instead",
							set.Position(asked.Pos()), who, calledName(call.Fun))
					}
					return true
				})
			}
			return true
		})
	}
	// A run that read nothing would pass for the wrong reason.
	if read == 0 {
		t.Fatal("no source files were read, so nothing was checked")
	}
}

// calledName is the name of what a call expression calls, without its
// receiver or package.
func calledName(fun ast.Expr) string {
	switch f := fun.(type) {
	case *ast.Ident:
		return f.Name
	case *ast.SelectorExpr:
		return f.Sel.Name
	}
	return ""
}
