package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrProviderNotFound is returned when no model provider carries that name.
var ErrProviderNotFound = errors.New("no such provider")

// ErrConversationNotFound is returned when no conversation carries that id.
var ErrConversationNotFound = errors.New("no such conversation")

// ChatProvider is a configured way to reach models (ADR-0022). The key itself
// is never here: CredentialRef says where the credential store keeps it.
type ChatProvider struct {
	// Name is the provider: `anthropic` or `openrouter`.
	Name string
	// BaseURL is where it is reached, empty for the provider's own default.
	BaseURL string
	// Models are the models it offers.
	Models []string
	// CredentialRef says where its key is kept.
	CredentialRef string
	// Created is when it was configured.
	Created time.Time
}

// Conversation is a chat the desktop app holds with a model.
type Conversation struct {
	ID    int64
	Title string
	// Model is what it was last spoken to, so it reopens where it was.
	Model            string
	Created, Updated time.Time
}

// ChatMessage is one turn of a conversation.
type ChatMessage struct {
	ID             int64
	ConversationID int64
	// Role is who said it: `user` or `assistant`.
	Role    string
	Text    string
	Created time.Time
}

// AddChatProvider records a model provider, replacing one of the same name:
// configuring it again is how a key is rotated.
func (s *Store) AddChatProvider(ctx context.Context, p ChatProvider) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO chat_providers (name, base_url, models, credential_ref, created)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(name) DO UPDATE SET base_url = excluded.base_url,
		   models = excluded.models, credential_ref = excluded.credential_ref`,
		p.Name, p.BaseURL, strings.Join(p.Models, "\n"), p.CredentialRef,
		p.Created.UTC().Format(timeFormat))
	return err
}

// ListChatProviders returns every provider, by name.
func (s *Store) ListChatProviders(ctx context.Context) ([]ChatProvider, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT name, base_url, models, credential_ref, created FROM chat_providers ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []ChatProvider
	for rows.Next() {
		p, err := scanChatProvider(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// GetChatProvider returns one by name, or ErrProviderNotFound.
func (s *Store) GetChatProvider(ctx context.Context, name string) (ChatProvider, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT name, base_url, models, credential_ref, created FROM chat_providers WHERE name = ?`, name)
	p, err := scanChatProvider(row)
	if errors.Is(err, sql.ErrNoRows) {
		return ChatProvider{}, fmt.Errorf("%w: %s", ErrProviderNotFound, name)
	}
	return p, err
}

// RemoveChatProvider takes one away, and reports whether there was one.
func (s *Store) RemoveChatProvider(ctx context.Context, name string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM chat_providers WHERE name = ?`, name)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

func scanChatProvider(row interface{ Scan(...any) error }) (ChatProvider, error) {
	var p ChatProvider
	var models, created string
	if err := row.Scan(&p.Name, &p.BaseURL, &models, &p.CredentialRef, &created); err != nil {
		return ChatProvider{}, err
	}
	if models != "" {
		p.Models = strings.Split(models, "\n")
	}
	t, err := time.Parse(timeFormat, created)
	if err != nil {
		return ChatProvider{}, err
	}
	p.Created = t
	return p, nil
}

// StartConversation records a new conversation and returns it with its id.
func (s *Store) StartConversation(ctx context.Context, c Conversation) (Conversation, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO conversations (title, model, created, updated) VALUES (?, ?, ?, ?)`,
		c.Title, c.Model, c.Created.UTC().Format(timeFormat), c.Updated.UTC().Format(timeFormat))
	if err != nil {
		return Conversation{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Conversation{}, err
	}
	c.ID = id
	return c, nil
}

// TouchConversation records that a conversation was spoken to, and what with.
func (s *Store) TouchConversation(ctx context.Context, id int64, model string, at time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE conversations SET updated = ?, model = ? WHERE id = ?`,
		at.UTC().Format(timeFormat), model, id)
	return err
}

// GetConversation returns one by id, or ErrConversationNotFound.
func (s *Store) GetConversation(ctx context.Context, id int64) (Conversation, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, title, model, created, updated FROM conversations WHERE id = ?`, id)
	c, err := scanConversation(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Conversation{}, fmt.Errorf("%w: %d", ErrConversationNotFound, id)
	}
	return c, err
}

// ListConversations returns every conversation, the most recently spoken to
// first, which is the order a reader wants them in.
func (s *Store) ListConversations(ctx context.Context) ([]Conversation, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, title, model, created, updated FROM conversations ORDER BY updated DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Conversation
	for rows.Next() {
		c, err := scanConversation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func scanConversation(row interface{ Scan(...any) error }) (Conversation, error) {
	var c Conversation
	var created, updated string
	if err := row.Scan(&c.ID, &c.Title, &c.Model, &created, &updated); err != nil {
		return Conversation{}, err
	}
	var err error
	if c.Created, err = time.Parse(timeFormat, created); err != nil {
		return Conversation{}, err
	}
	if c.Updated, err = time.Parse(timeFormat, updated); err != nil {
		return Conversation{}, err
	}
	return c, nil
}

// AddChatMessage records one turn of a conversation.
func (s *Store) AddChatMessage(ctx context.Context, m ChatMessage) (ChatMessage, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO chat_messages (conversation_id, role, text, created) VALUES (?, ?, ?, ?)`,
		m.ConversationID, m.Role, m.Text, m.Created.UTC().Format(timeFormat))
	if err != nil {
		return ChatMessage{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return ChatMessage{}, err
	}
	m.ID = id
	return m, nil
}

// ListChatMessages returns a conversation's turns, oldest first. Ordering is
// by id rather than by the time it records: a timestamp stored as text does
// not sort as a time.
func (s *Store) ListChatMessages(ctx context.Context, conversation int64) ([]ChatMessage, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, conversation_id, role, text, created FROM chat_messages
		 WHERE conversation_id = ? ORDER BY id`, conversation)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []ChatMessage
	for rows.Next() {
		var m ChatMessage
		var created string
		if err := rows.Scan(&m.ID, &m.ConversationID, &m.Role, &m.Text, &created); err != nil {
			return nil, err
		}
		t, err := time.Parse(timeFormat, created)
		if err != nil {
			return nil, err
		}
		m.Created = t
		out = append(out, m)
	}
	return out, rows.Err()
}
