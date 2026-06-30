package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/oniharnantyo/onclaw/internal/store"
)

// sqliteConversationStore implements store.ConversationStore.
type sqliteConversationStore struct {
	db *sql.DB
}

// NewConversationStore creates a new ConversationStore backed by SQLite.
func NewConversationStore(db *sql.DB) store.ConversationStore {
	return &sqliteConversationStore{db: db}
}

func (s *sqliteConversationStore) CreateConversation(ctx context.Context, agentName string) (int64, error) {
	t := now()
	res, err := s.db.ExecContext(ctx,
		"INSERT INTO conversations (agent_name, summary_until_seq, summary_message_id, created_at, updated_at) VALUES (?, 0, 0, ?, ?)",
		agentName, t, t,
	)
	if err != nil {
		return 0, fmt.Errorf("create conversation: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("get conversation id: %w", err)
	}
	return id, nil
}

func (s *sqliteConversationStore) AppendMessage(ctx context.Context, conversationID int64, role string, messageJSON string) (int64, error) {
	t := now()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	var seq int64
	err = tx.QueryRowContext(ctx,
		`INSERT INTO conversation_messages (conversation_id, seq, role, message, created_at)
		 VALUES (?, (SELECT COALESCE(MAX(seq), 0) + 1 FROM conversation_messages WHERE conversation_id = ?), ?, ?, ?)
		 RETURNING seq`,
		conversationID, conversationID, role, messageJSON, t,
	).Scan(&seq)
	if err != nil {
		return 0, fmt.Errorf("append message row: %w", err)
	}

	_, err = tx.ExecContext(ctx, "UPDATE conversations SET updated_at = ? WHERE id = ?", t, conversationID)
	if err != nil {
		return 0, fmt.Errorf("update conversation updated_at: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit append message: %w", err)
	}

	return seq, nil
}

func (s *sqliteConversationStore) LoadHistory(ctx context.Context, conversationID int64) (*store.MessageRow, []*store.MessageRow, error) {
	var summaryMessageID sql.NullInt64
	var summaryUntilSeq int64
	err := s.db.QueryRowContext(ctx,
		"SELECT summary_message_id, summary_until_seq FROM conversations WHERE id = ?",
		conversationID,
	).Scan(&summaryMessageID, &summaryUntilSeq)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil, fmt.Errorf("conversation not found: %d", conversationID)
		}
		return nil, nil, fmt.Errorf("get conversation meta: %w", err)
	}

	var summary *store.MessageRow
	if summaryMessageID.Valid && summaryMessageID.Int64 != 0 {
		var row store.MessageRow
		err = s.db.QueryRowContext(ctx,
			"SELECT id, conversation_id, seq, role, message, created_at FROM conversation_messages WHERE id = ?",
			summaryMessageID.Int64,
		).Scan(&row.ID, &row.ConversationID, &row.Seq, &row.Role, &row.Message, &row.CreatedAt)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, nil, fmt.Errorf("load summary message: %w", err)
		}
		if err == nil {
			summary = &row
		}
	}

	var summaryMsgIDVal int64
	if summaryMessageID.Valid {
		summaryMsgIDVal = summaryMessageID.Int64
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT id, conversation_id, seq, role, message, created_at
		 FROM conversation_messages
		 WHERE conversation_id = ? AND seq > ? AND id <> ?
		 ORDER BY seq ASC`,
		conversationID, summaryUntilSeq, summaryMsgIDVal,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("query tail messages: %w", err)
	}
	defer rows.Close()

	var tail []*store.MessageRow
	for rows.Next() {
		var row store.MessageRow
		if err := rows.Scan(&row.ID, &row.ConversationID, &row.Seq, &row.Role, &row.Message, &row.CreatedAt); err != nil {
			return nil, nil, fmt.Errorf("scan tail message: %w", err)
		}
		tail = append(tail, &row)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("rows error: %w", err)
	}

	return summary, tail, nil
}

func (s *sqliteConversationStore) ListMessages(ctx context.Context, conversationID int64) ([]*store.MessageRow, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, conversation_id, seq, role, message, created_at
		 FROM conversation_messages
		 WHERE conversation_id = ?
		 ORDER BY seq ASC`,
		conversationID,
	)
	if err != nil {
		return nil, fmt.Errorf("list messages: %w", err)
	}
	defer rows.Close()

	var msgs []*store.MessageRow
	for rows.Next() {
		var row store.MessageRow
		if err := rows.Scan(&row.ID, &row.ConversationID, &row.Seq, &row.Role, &row.Message, &row.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan message: %w", err)
		}
		msgs = append(msgs, &row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}
	return msgs, nil
}

func (s *sqliteConversationStore) SaveSummary(ctx context.Context, conversationID int64, summaryMessageJSON string, coveredUntilSeq int64) error {
	var msgRole struct {
		Role string `json:"role"`
	}
	if err := json.Unmarshal([]byte(summaryMessageJSON), &msgRole); err != nil {
		return fmt.Errorf("unmarshal summary message role: %w", err)
	}
	if msgRole.Role == "" {
		msgRole.Role = "assistant"
	}

	t := now()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	var summaryMessageID int64
	err = tx.QueryRowContext(ctx,
		`INSERT INTO conversation_messages (conversation_id, seq, role, message, created_at)
		 VALUES (?, (SELECT COALESCE(MAX(seq), 0) + 1 FROM conversation_messages WHERE conversation_id = ?), ?, ?, ?)
		 RETURNING id`,
		conversationID, conversationID, msgRole.Role, summaryMessageJSON, t,
	).Scan(&summaryMessageID)
	if err != nil {
		return fmt.Errorf("insert summary message row: %w", err)
	}

	_, err = tx.ExecContext(ctx,
		`UPDATE conversations
		 SET summary_message_id = ?, summary_until_seq = ?, updated_at = ?
		 WHERE id = ?`,
		summaryMessageID, coveredUntilSeq, t, conversationID,
	)
	if err != nil {
		return fmt.Errorf("update conversation with summary: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}

func (s *sqliteConversationStore) ListConversations(ctx context.Context) ([]*store.ConversationRow, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT c.id, c.agent_name, c.created_at, c.updated_at, COUNT(m.id) AS message_count
		 FROM conversations c
		 LEFT JOIN conversation_messages m ON c.id = m.conversation_id
		 GROUP BY c.id
		 ORDER BY c.updated_at DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("query conversations: %w", err)
	}
	defer rows.Close()

	var result []*store.ConversationRow
	for rows.Next() {
		var row store.ConversationRow
		if err := rows.Scan(&row.ID, &row.AgentName, &row.CreatedAt, &row.UpdatedAt, &row.MessageCount); err != nil {
			return nil, fmt.Errorf("scan conversation row: %w", err)
		}
		result = append(result, &row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}
	return result, nil
}
