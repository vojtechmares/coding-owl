package cli

import (
	"strings"
	"testing"

	"github.com/vojtechmares/coding-owl/internal/chat"
)

// TestProvidersSupportedNotesEveryProvider guards the one part of
// owl providers supported that is not derived from what chat holds. Which
// providers there are belongs to internal/chat; the line describing each one
// is written here, so a provider added there would otherwise print a blank
// cell and nobody would notice until a user read it.
func TestProvidersSupportedNotesEveryProvider(t *testing.T) {
	for _, name := range chat.Providers {
		if strings.TrimSpace(providerAbout[name]) == "" {
			t.Errorf("provider %q has no line in providerAbout, so owl providers supported prints a blank one for it", name)
		}
	}
	for name := range providerAbout {
		if !known(name) {
			t.Errorf("providerAbout describes %q, which is not a provider Owl drives", name)
		}
	}
}

// known is what chat.Providers holds, asked as a question. It is here rather
// than borrowed from chat because chat's own is unexported.
func known(name string) bool {
	for _, p := range chat.Providers {
		if p == name {
			return true
		}
	}
	return false
}
