// Package chat is the conversation the desktop app holds with a model
// (ADR-0022). The daemon holds the model credentials and keeps the
// conversations, so the app never carries either; what the model may do is
// read what Owl knows, and nothing else - it never acts.
package chat

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/vojtechmares/coding-owl/internal/credential"
	"github.com/vojtechmares/coding-owl/internal/store"
)

// InvalidError marks a failure the user can fix by asking for something
// different: a provider Owl does not drive, a model nobody configured, a
// conversation that is not there.
type InvalidError struct{ Err error }

func (e *InvalidError) Error() string { return e.Err.Error() }
func (e *InvalidError) Unwrap() error { return e.Err }

func invalid(format string, a ...any) error {
	return &InvalidError{Err: fmt.Errorf(format, a...)}
}

// Provider names a way to reach models. The set is closed: each one is a
// client Owl has written (ADR-0022).
const (
	// Anthropic is the Anthropic API, spoken directly.
	Anthropic = "anthropic"
	// OpenRouter is OpenRouter, which speaks the OpenAI shape for any model
	// it offers.
	OpenRouter = "openrouter"
)

// Providers is every provider Owl drives, in the order a listing shows them.
var Providers = []string{Anthropic, OpenRouter}

// DefaultModels is what a provider offers when the user named no model. Only
// Anthropic has an answer: what OpenRouter offers is the user's own account's
// business, so it is configured.
var DefaultModels = map[string][]string{
	Anthropic: {"claude-opus-5", "claude-sonnet-5", "claude-haiku-4-5-20251001"},
}

// modelRE is what a model may be called. It goes on the wire to a provider and
// comes back in a listing, so it is kept to what both read the same way.
var modelRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9./:_-]*$`)

// maxTitle is how many characters of the first thing said become a
// conversation's title. It is counted in characters rather than bytes, so a
// title in a script that is not ASCII is not a third of the length.
const maxTitle = 80

// Config is a configured provider as the user sees it. The key is never here.
type Config struct {
	// Name is the provider.
	Name string
	// BaseURL is where it is reached, empty for the provider's own default.
	BaseURL string
	// Models are the models it offers.
	Models []string
	// Created is when it was configured.
	Created time.Time
}

// Model is one model a user may speak to, and the provider it comes from.
type Model struct {
	Provider string
	ID       string
}

// Conversation is a chat, as it is listed.
type Conversation struct {
	ID               int64
	Title            string
	Model            string
	Created, Updated time.Time
}

// Message is one turn of a conversation.
type Message struct {
	Role    string
	Text    string
	Created time.Time
}

// Roles a message may carry.
const (
	RoleUser      = "user"
	RoleAssistant = "assistant"
)

// Store is what the chat keeps its conversations and providers in.
type Store interface {
	AddChatProvider(ctx context.Context, p store.ChatProvider) error
	ListChatProviders(ctx context.Context) ([]store.ChatProvider, error)
	GetChatProvider(ctx context.Context, name string) (store.ChatProvider, error)
	RemoveChatProvider(ctx context.Context, name string) (bool, error)
	StartConversation(ctx context.Context, c store.Conversation) (store.Conversation, error)
	TouchConversation(ctx context.Context, id int64, model string, at time.Time) error
	GetConversation(ctx context.Context, id int64) (store.Conversation, error)
	ListConversations(ctx context.Context) ([]store.Conversation, error)
	AddChatMessage(ctx context.Context, m store.ChatMessage) (store.ChatMessage, error)
	ListChatMessages(ctx context.Context, conversation int64) ([]store.ChatMessage, error)
}

// Service is the chat.
type Service struct {
	store  Store
	creds  credential.Store
	tools  *Tools
	client func(p store.ChatProvider, key string) (Client, error)
	now    func() time.Time
}

// NewService returns a chat over that store, credential store and view of what
// Owl knows.
func NewService(st Store, creds credential.Store, tools *Tools) *Service {
	return &Service{store: st, creds: creds, tools: tools, client: NewClient, now: time.Now}
}

// AddProvider configures a way to reach models, putting the key in the
// credential store and only a reference to it in the database (ADR-0019).
// Configuring one that is already there replaces it, which is how a key is
// rotated.
func (s *Service) AddProvider(ctx context.Context, name, key, baseURL string, models []string) (Config, error) {
	var err error
	name = strings.ToLower(strings.TrimSpace(name))
	if !known(name) {
		return Config{}, invalid("%q is not a provider Owl drives; it drives %s",
			name, strings.Join(Providers, " and "))
	}
	if strings.TrimSpace(key) == "" {
		return Config{}, invalid("a provider needs a key to reach it with")
	}
	if err := checkBaseURL(baseURL); err != nil {
		return Config{}, err
	}
	models, err = checkModels(name, models)
	if err != nil {
		return Config{}, err
	}
	ref := credentialRef(name)
	// Whether this is a first configuration or a rotation decides what happens
	// if the row cannot be written: a rotation's key is the one the provider
	// that is still configured uses, so the one it had is kept to put back.
	_, err = s.store.GetChatProvider(ctx, name)
	rotating := err == nil
	if err != nil && !errors.Is(err, store.ErrProviderNotFound) {
		return Config{}, err
	}
	var had string
	if rotating {
		had, _ = s.creds.Get(ctx, ref)
	}
	if err := s.creds.Set(ctx, ref, strings.TrimSpace(key)); err != nil {
		return Config{}, err
	}
	p := store.ChatProvider{
		Name: name, BaseURL: strings.TrimSpace(baseURL), Models: models,
		CredentialRef: ref, Created: s.now().UTC(),
	}
	if err := s.store.AddChatProvider(ctx, p); err != nil {
		// The row is what makes the key reachable, so a key without one is
		// taken back rather than left in the store. A rotation that did not
		// happen puts back the key of the provider that is still configured,
		// rather than leaving it reaching models with one nobody asked it to.
		switch {
		case !rotating:
			_ = s.creds.Delete(ctx, ref)
		case had != "":
			_ = s.creds.Set(ctx, ref, had)
		default:
			// The key it had could not be read, so there is nothing to put
			// back. That leaves it reaching models with the new one, which the
			// user is told rather than left to find out.
			return Config{}, fmt.Errorf("%w; %s is configured as it was but now "+
				"reaches models with the new key", err, name)
		}
		return Config{}, err
	}
	return asConfig(p), nil
}

// ListProviders reports what is configured, without any key.
func (s *Service) ListProviders(ctx context.Context) ([]Config, error) {
	rows, err := s.store.ListChatProviders(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Config, 0, len(rows))
	for _, p := range rows {
		out = append(out, asConfig(p))
	}
	return out, nil
}

// RemoveProvider takes a provider and its key away.
func (s *Service) RemoveProvider(ctx context.Context, name string) error {
	name = strings.ToLower(strings.TrimSpace(name))
	p, err := s.store.GetChatProvider(ctx, name)
	if errors.Is(err, store.ErrProviderNotFound) {
		return invalid("no provider called %s is configured", name)
	}
	if err != nil {
		return err
	}
	if _, err := s.store.RemoveChatProvider(ctx, name); err != nil {
		return err
	}
	// The key goes with it: a secret nothing can reach is a secret nobody
	// meant to keep (ADR-0019).
	return s.creds.Delete(ctx, p.CredentialRef)
}

// Models is every model the configured providers offer.
func (s *Service) Models(ctx context.Context) ([]Model, error) {
	configs, err := s.ListProviders(ctx)
	if err != nil {
		return nil, err
	}
	var out []Model
	for _, c := range configs {
		for _, id := range c.Models {
			out = append(out, Model{Provider: c.Name, ID: id})
		}
	}
	return out, nil
}

// Conversations lists them, the most recently spoken to first.
func (s *Service) Conversations(ctx context.Context) ([]Conversation, error) {
	rows, err := s.store.ListConversations(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Conversation, 0, len(rows))
	for _, c := range rows {
		out = append(out, asConversation(c))
	}
	return out, nil
}

// Conversation is one conversation with what was said in it.
func (s *Service) Conversation(ctx context.Context, id int64) (Conversation, []Message, error) {
	c, err := s.store.GetConversation(ctx, id)
	if errors.Is(err, store.ErrConversationNotFound) {
		return Conversation{}, nil, invalid("no conversation %d", id)
	}
	if err != nil {
		return Conversation{}, nil, err
	}
	rows, err := s.store.ListChatMessages(ctx, id)
	if err != nil {
		return Conversation{}, nil, err
	}
	out := make([]Message, 0, len(rows))
	for _, m := range rows {
		out = append(out, Message{Role: m.Role, Text: m.Text, Created: m.Created})
	}
	return asConversation(c), out, nil
}

// known reports whether Owl drives that provider.
func known(name string) bool {
	for _, p := range Providers {
		if p == name {
			return true
		}
	}
	return false
}

// credentialRef is where a provider's key is kept.
func credentialRef(name string) string { return "chat/" + name }

// checkBaseURL refuses what is not somewhere Owl can send a request.
func checkBaseURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	at, err := url.Parse(raw)
	if err != nil {
		return invalid("the base url %q is not one: %v", raw, err)
	}
	if at.Scheme != "https" && at.Scheme != "http" {
		return invalid("the base url %q is not http or https", raw)
	}
	if at.Host == "" {
		return invalid("the base url %q names no host", raw)
	}
	// A url that carries a name and password would put them in the database
	// and in a listing, which is not where a credential lives (ADR-0019).
	if at.User != nil {
		return invalid("the base url carries a name or a password; a provider's key is configured with --key-stdin")
	}
	// Every request is that url with a path put after it, so anything that
	// cannot come before a path would end up in the middle of one.
	if at.RawQuery != "" || at.ForceQuery || at.Fragment != "" {
		return invalid("the base url %q carries a query or a fragment; it is where Owl reaches the provider, "+
			"and every request puts a path after it", raw)
	}
	return nil
}

// checkModels is the models a provider offers: what the user named, or what
// Owl knows the provider has. A provider with neither is refused rather than
// configured with nothing to speak to.
func checkModels(name string, models []string) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	for _, m := range models {
		m = strings.TrimSpace(m)
		if m == "" {
			continue
		}
		if !modelRE.MatchString(m) {
			return nil, invalid("%q is not a model name Owl can use", m)
		}
		if !seen[m] {
			seen[m] = true
			out = append(out, m)
		}
	}
	if len(out) > 0 {
		return out, nil
	}
	if defaults := DefaultModels[name]; len(defaults) > 0 {
		return append([]string(nil), defaults...), nil
	}
	return nil, invalid("%s offers whatever your account has, so name the models with --model", name)
}

func asConfig(p store.ChatProvider) Config {
	return Config{Name: p.Name, BaseURL: p.BaseURL, Models: p.Models, Created: p.Created}
}

func asConversation(c store.Conversation) Conversation {
	return Conversation{ID: c.ID, Title: c.Title, Model: c.Model, Created: c.Created, Updated: c.Updated}
}

// titleOf is what a conversation is called: the first thing said in it, cut to
// something a list can show. The text is the user's, so it is cut at a
// character and not in the middle of one.
func titleOf(text string) string {
	title := strings.ToValidUTF8(strings.TrimSpace(strings.ReplaceAll(text, "\n", " ")), "")
	runes := []rune(title)
	if len(runes) <= maxTitle {
		return title
	}
	cut := string(runes[:maxTitle])
	if at := strings.LastIndexByte(cut, ' '); at > len(cut)/2 {
		cut = cut[:at]
	}
	return cut + "..."
}
