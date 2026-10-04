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
