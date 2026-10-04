package repository

import (
	"context"
	"database/sql"
	"fmt"
)

// ChatsRepo provides minimal chat/message access (Phase 1: storage layer
// only; chat endpoints and SSE arrive in a later phase).
type ChatsRepo struct {
	db *sql.DB
}

// NewChatsRepo builds a ChatsRepo on db.
func NewChatsRepo(db *sql.DB) *ChatsRepo { return &ChatsRepo{db: db} }

// ChatRow is one chat session.
type ChatRow struct {
	ID                  string
	Title               string
	ConversationContext string
	CompactSummary      string
	CreatedAt           string
	UpdatedAt           string
}

// MessageRow is one chat message.
type MessageRow struct {
	ID               string
	ChatID           string
	Role             string
	Content          string
	ReasoningContent string
	CreatedAt        string
	GenerationID     sql.NullString
}

// InsertChat creates a chat row.
func (r *ChatsRepo) InsertChat(ctx context.Context, row ChatRow) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO chats(id, title, conversation_context, compact_summary, created_at, updated_at)
		 VALUES(?, ?, ?, ?, ?, ?)`,
		row.ID, row.Title, row.ConversationContext, row.CompactSummary, row.CreatedAt, row.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("chats insert: %w", err)
	}
	return nil
}

// GetChat returns the chat with id, or ErrNotFound.
func (r *ChatsRepo) GetChat(ctx context.Context, id string) (ChatRow, error) {
	var row ChatRow
	err := r.db.QueryRowContext(ctx,
		`SELECT id, title, conversation_context, compact_summary, created_at, updated_at
		 FROM chats WHERE id = ?`, id,
	).Scan(&row.ID, &row.Title, &row.ConversationContext, &row.CompactSummary, &row.CreatedAt, &row.UpdatedAt)
	if err == sql.ErrNoRows {
		return ChatRow{}, ErrNotFound
	}
	if err != nil {
		return ChatRow{}, fmt.Errorf("chats get: %w", err)
	}
	return row, nil
}

// DeleteChat removes a chat; messages disappear via ON DELETE CASCADE.
func (r *ChatsRepo) DeleteChat(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM chats WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("chats delete: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return ErrNotFound
	}
	return nil
}

// InsertMessage appends a message to a chat.
func (r *ChatsRepo) InsertMessage(ctx context.Context, row MessageRow) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO messages(id, chat_id, role, content, reasoning_content, created_at, generation_id)
		 VALUES(?, ?, ?, ?, ?, ?, ?)`,
		row.ID, row.ChatID, row.Role, row.Content, row.ReasoningContent, row.CreatedAt, row.GenerationID,
	)
	if err != nil {
		return fmt.Errorf("messages insert: %w", err)
	}
	return nil
}

// ListMessages returns the messages of a chat ordered by (created_at, id).
func (r *ChatsRepo) ListMessages(ctx context.Context, chatID string) ([]MessageRow, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, chat_id, role, content, reasoning_content, created_at, generation_id
		 FROM messages WHERE chat_id = ? ORDER BY created_at, id`, chatID,
	)
	if err != nil {
		return nil, fmt.Errorf("messages list: %w", err)
	}
	defer rows.Close()
	var out []MessageRow
	for rows.Next() {
		var row MessageRow
		if err := rows.Scan(&row.ID, &row.ChatID, &row.Role, &row.Content, &row.ReasoningContent,
			&row.CreatedAt, &row.GenerationID); err != nil {
			return nil, fmt.Errorf("messages list scan: %w", err)
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// CountMessages returns the number of messages in a chat.
func (r *ChatsRepo) CountMessages(ctx context.Context, chatID string) (int, error) {
	var n int
	if err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM messages WHERE chat_id = ?`, chatID,
	).Scan(&n); err != nil {
		return 0, fmt.Errorf("messages count: %w", err)
	}
	return n, nil
}

// ListChats returns every chat ordered by updated_at DESC (newest activity
// first, docs/04 §10). The id DESC tiebreak keeps the order stable when two
// chats share the same RFC3339 second.
func (r *ChatsRepo) ListChats(ctx context.Context) ([]ChatRow, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, title, conversation_context, compact_summary, created_at, updated_at
		 FROM chats ORDER BY updated_at DESC, id DESC`)
	if err != nil {
		return nil, fmt.Errorf("chats list: %w", err)
	}
	defer rows.Close()
	var out []ChatRow
	for rows.Next() {
		var row ChatRow
		if err := rows.Scan(&row.ID, &row.Title, &row.ConversationContext, &row.CompactSummary,
			&row.CreatedAt, &row.UpdatedAt); err != nil {
			return nil, fmt.Errorf("chats list scan: %w", err)
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// UpdateChatTitle renames a chat and bumps updated_at. ErrNotFound when the
// chat does not exist.
func (r *ChatsRepo) UpdateChatTitle(ctx context.Context, id, title, updatedAt string) error {
	return r.execChatUpdate(ctx, "title",
		`UPDATE chats SET title = ?, updated_at = ? WHERE id = ?`, title, updatedAt, id)
}

// UpdateChatContext stores the conversation context and bumps updated_at.
func (r *ChatsRepo) UpdateChatContext(ctx context.Context, id, conversationContext, updatedAt string) error {
	return r.execChatUpdate(ctx, "context",
		`UPDATE chats SET conversation_context = ?, updated_at = ? WHERE id = ?`, conversationContext, updatedAt, id)
}

// SetCompactSummary stores the compact summary and bumps updated_at.
func (r *ChatsRepo) SetCompactSummary(ctx context.Context, id, summary, updatedAt string) error {
	return r.execChatUpdate(ctx, "compact summary",
		`UPDATE chats SET compact_summary = ?, updated_at = ? WHERE id = ?`, summary, updatedAt, id)
}

// TouchChat bumps updated_at only (message activity marker).
func (r *ChatsRepo) TouchChat(ctx context.Context, id, updatedAt string) error {
	return r.execChatUpdate(ctx, "touch",
		`UPDATE chats SET updated_at = ? WHERE id = ?`, updatedAt, id)
}

// execChatUpdate runs one single-row chat UPDATE, mapping zero rows affected
// to ErrNotFound.
func (r *ChatsRepo) execChatUpdate(ctx context.Context, what, query string, args ...any) error {
	res, err := r.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("chats update %s: %w", what, err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteMessage removes one message by id. Deleting an already-deleted id is
// a no-op (nil), matching the regenerate flow's best-effort semantics.
func (r *ChatsRepo) DeleteMessage(ctx context.Context, id string) error {
	if _, err := r.db.ExecContext(ctx, `DELETE FROM messages WHERE id = ?`, id); err != nil {
		return fmt.Errorf("messages delete: %w", err)
	}
	return nil
}

// DeleteMessages removes every message of a chat (the /clear primitive).
func (r *ChatsRepo) DeleteMessages(ctx context.Context, chatID string) error {
	if _, err := r.db.ExecContext(ctx, `DELETE FROM messages WHERE chat_id = ?`, chatID); err != nil {
		return fmt.Errorf("messages delete all: %w", err)
	}
	return nil
}

// DeleteAllChats removes every chat; messages disappear via
// ON DELETE CASCADE (clear business data, docs/05 §7).
func (r *ChatsRepo) DeleteAllChats(ctx context.Context) error {
	if _, err := r.db.ExecContext(ctx, `DELETE FROM chats`); err != nil {
		return fmt.Errorf("chats delete all: %w", err)
	}
	return nil
}

// ListAllMessages returns every message across all chats ordered by
// (created_at, id) — the backup export view.
func (r *ChatsRepo) ListAllMessages(ctx context.Context) ([]MessageRow, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, chat_id, role, content, reasoning_content, created_at, generation_id
		 FROM messages ORDER BY created_at, id`)
	if err != nil {
		return nil, fmt.Errorf("messages list all: %w", err)
	}
	defer rows.Close()
	var out []MessageRow
	for rows.Next() {
		var row MessageRow
		if err := rows.Scan(&row.ID, &row.ChatID, &row.Role, &row.Content, &row.ReasoningContent,
			&row.CreatedAt, &row.GenerationID); err != nil {
			return nil, fmt.Errorf("messages list all scan: %w", err)
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// MessageExists reports whether a message with id is stored (backup import
// dedupe).
func (r *ChatsRepo) MessageExists(ctx context.Context, id string) (bool, error) {
	var one int
	err := r.db.QueryRowContext(ctx, `SELECT 1 FROM messages WHERE id = ?`, id).Scan(&one)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("messages exists: %w", err)
	}
	return true, nil
}

// ChatExists reports whether a chat with id is stored (backup import dedupe).
func (r *ChatsRepo) ChatExists(ctx context.Context, id string) (bool, error) {
	var one int
	err := r.db.QueryRowContext(ctx, `SELECT 1 FROM chats WHERE id = ?`, id).Scan(&one)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("chats exists: %w", err)
	}
	return true, nil
}
