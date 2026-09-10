package config_test

import (
	"strings"
	"testing"

	"github.com/vojtechmares/coding-owl/internal/config"
)

func TestParseReadsBranchPrefix(t *testing.T) {
	cfg, err := config.Parse("main:.coding-owl.yaml", []byte("apiVersion: codingowl.dev/v1\nbranchPrefix: root/\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.BranchPrefix != "root/" {
		t.Errorf("BranchPrefix = %q, want %q", cfg.BranchPrefix, "root/")
	}
}

func TestParseRefusesMissingAPIVersion(t *testing.T) {
	_, err := config.Parse("main:.coding-owl.yaml", []byte("branchPrefix: nope/\n"))
	if err == nil {
		t.Fatal("Parse accepted a file with no apiVersion")
	}
	for _, want := range []string{"main:.coding-owl.yaml", "apiVersion"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

func TestParseRefusesUnknownAPIVersion(t *testing.T) {
	_, err := config.Parse("main:.coding-owl.yaml", []byte("apiVersion: codingowl.dev/v99\nbranchPrefix: nope/\n"))
	if err == nil {
		t.Fatal("Parse accepted an unrecognised apiVersion")
	}
	for _, want := range []string{"main:.coding-owl.yaml", "apiVersion", "codingowl.dev/v99"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

func TestDefaultBranchPrefix(t *testing.T) {
	if got := config.Default().BranchPrefix; got != "owl/" {
		t.Errorf("Default().BranchPrefix = %q, want %q", got, "owl/")
	}
}

func TestParseFillsDefaultsForOmittedKeys(t *testing.T) {
	cfg, err := config.Parse("main:.coding-owl.yaml", []byte("apiVersion: codingowl.dev/v1\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.BranchPrefix != config.Default().BranchPrefix {
		t.Errorf("BranchPrefix = %q, want the default %q", cfg.BranchPrefix, config.Default().BranchPrefix)
	}
}

func TestParseRefusesABranchPrefixThatWouldReadAsAnOption(t *testing.T) {
	_, err := config.Parse("main:.coding-owl.yaml",
		[]byte("apiVersion: codingowl.dev/v1\nbranchPrefix: -f/\n"))

	if err == nil {
		t.Fatal("Parse with a dash-leading branchPrefix = nil, want an error")
	}
	if !strings.Contains(err.Error(), "-f/") || !strings.Contains(err.Error(), "dash") {
		t.Errorf("error %q does not say what is wrong with the prefix", err)
	}
}

func TestParseReadsAProjectsClausesAndSpendCap(t *testing.T) {
	cfg, err := config.Parse("main:.coding-owl.yaml", []byte(
		"apiVersion: codingowl.dev/v1\nunattendedClauses:\n  - run gofmt\nbudgetUSD: 2.5\n"))

	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(cfg.UnattendedClauses) != 1 || cfg.UnattendedClauses[0] != "run gofmt" {
		t.Errorf("clauses = %v, want the one the file carries", cfg.UnattendedClauses)
	}
	if cfg.BudgetUSD != 2.5 {
		t.Errorf("budget = %v, want 2.5", cfg.BudgetUSD)
	}
	if _, err := config.Parse("f", []byte("apiVersion: codingowl.dev/v1\nbudgetUSD: -1\n")); err == nil {
		t.Error("a negative spend cap was accepted")
	}
}
