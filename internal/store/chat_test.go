package store_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/vojtechmares/coding-owl/internal/store"
)

func TestChatProviderRoundTripsWithoutItsKey(t *testing.T) {
	st := openStore(t, filepath.Join(t.TempDir(), "owl.db"))
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	if err := st.AddChatProvider(ctx, store.ChatProvider{
		Name: "openrouter", BaseURL: "https://openrouter.ai/api/v1",
		Models:        []string{"openai/gpt-5", "anthropic/claude-sonnet-4.5"},
		CredentialRef: "chat/openrouter", Created: now,
	}); err != nil {
		t.Fatalf("AddChatProvider: %v", err)
	}

	got, err := st.GetChatProvider(ctx, "openrouter")
	if err != nil {
		t.Fatalf("GetChatProvider: %v", err)
	}
	if len(got.Models) != 2 || got.Models[0] != "openai/gpt-5" {
		t.Errorf("Models = %v, want what was configured in order", got.Models)
	}
	if got.CredentialRef != "chat/openrouter" {
		t.Errorf("CredentialRef = %q", got.CredentialRef)
	}

	// Configuring it again is how a key is rotated, not a second provider.
	if err := st.AddChatProvider(ctx, store.ChatProvider{
		Name: "openrouter", Models: []string{"openai/gpt-5"},
		CredentialRef: "chat/openrouter", Created: now,
	}); err != nil {
		t.Fatalf("AddChatProvider again: %v", err)
	}
	all, err := st.ListChatProviders(ctx)
	if err != nil || len(all) != 1 {
		t.Fatalf("ListChatProviders = %+v, %v; want one", all, err)
	}

	gone, err := st.RemoveChatProvider(ctx, "openrouter")
	if err != nil || !gone {
		t.Fatalf("RemoveChatProvider = %v, %v", gone, err)
	}
	if again, err := st.RemoveChatProvider(ctx, "openrouter"); err != nil || again {
		t.Errorf("removing one that is not there = %v, %v", again, err)
	}
}

func TestConversationsAreListedMostRecentlySpokenToFirst(t *testing.T) {
	st := openStore(t, filepath.Join(t.TempDir(), "owl.db"))
	ctx := context.Background()
	start := time.Now().UTC().Truncate(time.Second)

	older, err := st.StartConversation(ctx, store.Conversation{
		Title: "older", Created: start, Updated: start,
	})
	if err != nil {
		t.Fatalf("StartConversation: %v", err)
	}
	newer, err := st.StartConversation(ctx, store.Conversation{
		Title: "newer", Created: start.Add(time.Minute), Updated: start.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("StartConversation: %v", err)
	}
	// The older one is spoken to last.
	if err := st.TouchConversation(ctx, older.ID, "claude", start.Add(2*time.Minute)); err != nil {
		t.Fatalf("TouchConversation: %v", err)
	}

	got, err := st.ListConversations(ctx)
	if err != nil {
		t.Fatalf("ListConversations: %v", err)
	}
	if len(got) != 2 || got[0].ID != older.ID || got[1].ID != newer.ID {
		t.Fatalf("ListConversations = %+v, want the most recently spoken to first", got)
	}
	if got[0].Model != "claude" {
		t.Errorf("the conversation records %q as what it was spoken to with", got[0].Model)
	}
}

func TestChatMessagesComeBackInTheOrderTheyWereSaid(t *testing.T) {
	st := openStore(t, filepath.Join(t.TempDir(), "owl.db"))
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	c, err := st.StartConversation(ctx, store.Conversation{Created: now, Updated: now})
	if err != nil {
		t.Fatalf("StartConversation: %v", err)
	}

	for _, m := range []store.ChatMessage{
		{ConversationID: c.ID, Role: "user", Text: "why was job 7 blocked", Created: now},
		{ConversationID: c.ID, Role: "assistant", Text: "the tests failed", Created: now},
	} {
		if _, err := st.AddChatMessage(ctx, m); err != nil {
			t.Fatalf("AddChatMessage: %v", err)
		}
	}

	got, err := st.ListChatMessages(ctx, c.ID)
	if err != nil {
		t.Fatalf("ListChatMessages: %v", err)
	}
	if len(got) != 2 || got[0].Role != "user" || got[1].Role != "assistant" {
		t.Fatalf("ListChatMessages = %+v, want what was said in order", got)
	}
	if _, err := st.GetConversation(ctx, c.ID+100); err == nil {
		t.Error("GetConversation of one that is not there returned no error")
	}
}
