package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

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

func (s *sqliteConversationStore) AppendTurn(
	ctx context.Context,
	convID int64,
	msgArrayJSON string,
	responseID string,
	previousResponseID string,
	model string,
	prompt int64,
	completion int64,
	total int64,
	question string,
	answer string,
) (int64, error) {
	t := now()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	var seq int64
	err = tx.QueryRowContext(ctx,
		`INSERT INTO conversation_messages (
			conversation_id, sequence_num, response_id, previous_response_id,
			message, model, prompt_tokens, completion_tokens, total_tokens,
			question, answer, created_at
		 )
		 VALUES (
			?,
			(SELECT COALESCE(MAX(sequence_num), 0) + 1 FROM conversation_messages WHERE conversation_id = ?),
			?, ?, ?, ?, ?, ?, ?, ?, ?, ?
		 )
		 RETURNING sequence_num`,
		convID, convID, responseID, previousResponseID,
		msgArrayJSON, model, prompt, completion, total,
		question, answer, t,
	).Scan(&seq)
	if err != nil {
		return 0, fmt.Errorf("append turn row: %w", err)
	}

	_, err = tx.ExecContext(ctx, "UPDATE conversations SET updated_at = ? WHERE id = ?", t, convID)
	if err != nil {
		return 0, fmt.Errorf("update conversation updated_at: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit append turn: %w", err)
	}

	return seq, nil
}

func (s *sqliteConversationStore) LoadHistory(ctx context.Context, conversationID int64) (*store.TurnRow, []*store.TurnRow, error) {
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

	var summary *store.TurnRow
	if summaryMessageID.Valid && summaryMessageID.Int64 != 0 {
		var row store.TurnRow
		err = s.db.QueryRowContext(ctx,
			`SELECT id, conversation_id, sequence_num, response_id, previous_response_id,
			        message, model, prompt_tokens, completion_tokens, total_tokens,
			        question, answer, created_at
			 FROM conversation_messages WHERE id = ?`,
			summaryMessageID.Int64,
		).Scan(&row.ID, &row.ConversationID, &row.SequenceNum, &row.ResponseID, &row.PreviousResponseID,
			&row.Message, &row.Model, &row.PromptTokens, &row.CompletionTokens, &row.TotalTokens,
			&row.Question, &row.Answer, &row.CreatedAt)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, nil, fmt.Errorf("load summary turn: %w", err)
		}
		if err == nil {
			summary = &row
		}
	}

	var summaryMsgIDVal int64
	if summaryMessageID.Valid {
		summaryMsgIDVal = summaryMessageID.Int64
	}

	var query string
	if summary != nil {
		query = `SELECT * FROM (
			SELECT id, conversation_id, sequence_num, response_id, previous_response_id,
			       message, model, prompt_tokens, completion_tokens, total_tokens,
			       question, answer, created_at
			FROM conversation_messages
			WHERE conversation_id = ? AND sequence_num > ? AND id <> ?
			ORDER BY sequence_num DESC
			LIMIT 3
		) ORDER BY sequence_num ASC`
	} else {
		query = `SELECT id, conversation_id, sequence_num, response_id, previous_response_id,
		        message, model, prompt_tokens, completion_tokens, total_tokens,
		        question, answer, created_at
		 FROM conversation_messages
		 WHERE conversation_id = ? AND sequence_num > ? AND id <> ?
		 ORDER BY sequence_num ASC`
	}

	rows, err := s.db.QueryContext(ctx, query, conversationID, summaryUntilSeq, summaryMsgIDVal)
	if err != nil {
		return nil, nil, fmt.Errorf("query tail turns: %w", err)
	}
	defer rows.Close()

	var tail []*store.TurnRow
	for rows.Next() {
		var row store.TurnRow
		if err := rows.Scan(&row.ID, &row.ConversationID, &row.SequenceNum, &row.ResponseID, &row.PreviousResponseID,
			&row.Message, &row.Model, &row.PromptTokens, &row.CompletionTokens, &row.TotalTokens,
			&row.Question, &row.Answer, &row.CreatedAt); err != nil {
			return nil, nil, fmt.Errorf("scan tail turn: %w", err)
		}
		tail = append(tail, &row)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("rows error: %w", err)
	}

	return summary, tail, nil
}

func (s *sqliteConversationStore) ListTurns(ctx context.Context, conversationID int64) ([]*store.TurnRow, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, conversation_id, sequence_num, response_id, previous_response_id,
		        message, model, prompt_tokens, completion_tokens, total_tokens,
		        question, answer, is_summary, created_at
		 FROM conversation_messages
		 WHERE conversation_id = ?
		 ORDER BY sequence_num ASC`,
		conversationID,
	)
	if err != nil {
		return nil, fmt.Errorf("list turns: %w", err)
	}
	defer rows.Close()

	var turns []*store.TurnRow
	for rows.Next() {
		var row store.TurnRow
		if err := rows.Scan(&row.ID, &row.ConversationID, &row.SequenceNum, &row.ResponseID, &row.PreviousResponseID,
			&row.Message, &row.Model, &row.PromptTokens, &row.CompletionTokens, &row.TotalTokens,
			&row.Question, &row.Answer, &row.IsSummary, &row.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan turn: %w", err)
		}
		turns = append(turns, &row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}
	return turns, nil
}

func (s *sqliteConversationStore) SaveSummary(ctx context.Context, conversationID int64, summaryMessageJSON string, coveredUntilSeq int64) error {
	var msg struct {
		Role          string `json:"role"`
		ContentBlocks []struct {
			AssistantGenText *struct {
				Text string `json:"text"`
			} `json:"assistant_gen_text"`
		} `json:"content_blocks"`
	}
	if err := json.Unmarshal([]byte(summaryMessageJSON), &msg); err != nil {
		return fmt.Errorf("unmarshal summary message: %w", err)
	}
	if msg.Role == "" {
		msg.Role = "assistant"
	}

	var answer string
	for _, block := range msg.ContentBlocks {
		if block.AssistantGenText != nil {
			if answer != "" {
				answer += "\n"
			}
			answer += block.AssistantGenText.Text
		}
	}

	var msgArray []json.RawMessage
	msgArray = append(msgArray, json.RawMessage(summaryMessageJSON))
	msgArrayJSONBytes, err := json.Marshal(msgArray)
	if err != nil {
		return fmt.Errorf("marshal summary message array: %w", err)
	}
	msgArrayJSON := string(msgArrayJSONBytes)

	t := now()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	var summaryMessageID int64
	err = tx.QueryRowContext(ctx,
		`INSERT INTO conversation_messages (
			conversation_id, sequence_num, message, question, answer, is_summary, created_at
		 )
		 VALUES (?, (SELECT COALESCE(MAX(sequence_num), 0) + 1 FROM conversation_messages WHERE conversation_id = ?), ?, '', ?, 1, ?)
		 RETURNING id`,
		conversationID, conversationID, msgArrayJSON, answer, t,
	).Scan(&summaryMessageID)
	if err != nil {
		return fmt.Errorf("insert summary turn row: %w", err)
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

func (s *sqliteConversationStore) GetCompactionMeta(ctx context.Context, conversationID int64) (int, string, error) {
	var count int
	var lastAt sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*), COALESCE(MAX(created_at), '')
		 FROM conversation_messages
		 WHERE conversation_id = ? AND is_summary = 1`,
		conversationID,
	).Scan(&count, &lastAt)
	if err != nil {
		return 0, "", fmt.Errorf("get compaction meta: %w", err)
	}
	return count, lastAt.String, nil
}

func (s *sqliteConversationStore) Transcript(ctx context.Context, conversationID int64, upToSeq int64) (string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT sequence_num, message, question, answer, created_at
		 FROM conversation_messages
		 WHERE conversation_id = ? AND sequence_num <= ? AND is_summary = 0
		 ORDER BY sequence_num ASC`,
		conversationID, upToSeq,
	)
	if err != nil {
		return "", fmt.Errorf("query transcript: %w", err)
	}
	defer rows.Close()

	var b strings.Builder
	for rows.Next() {
		var seq int64
		var message, question, answer, createdAt string
		if err := rows.Scan(&seq, &message, &question, &answer, &createdAt); err != nil {
			return "", fmt.Errorf("scan transcript row: %w", err)
		}
		b.WriteString(fmt.Sprintf("--- Turn %d (%s) ---\n", seq, createdAt))
		if question != "" {
			b.WriteString("User: " + question + "\n")
		}
		if answer != "" {
			b.WriteString("Assistant: " + answer + "\n")
		}
		// Include the exact message array so the agent can recover tool calls
		// and other structured detail the summary abbreviates.
		b.WriteString(message + "\n\n")
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("rows error: %w", err)
	}
	return b.String(), nil
}

func (s *sqliteConversationStore) ListConversations(ctx context.Context) ([]*store.ConversationRow, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT c.id, c.agent_name, c.created_at, c.updated_at, COUNT(m.id) AS message_count,
		        COALESCE((SELECT question FROM conversation_messages WHERE conversation_id = c.id ORDER BY sequence_num ASC LIMIT 1), '') AS preview
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
		if err := rows.Scan(&row.ID, &row.AgentName, &row.CreatedAt, &row.UpdatedAt, &row.MessageCount, &row.Preview); err != nil {
			return nil, fmt.Errorf("scan conversation row: %w", err)
		}
		result = append(result, &row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}
	return result, nil
}
