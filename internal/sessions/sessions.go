package sessions

import (
	"context"
	"errors"
	"fmt"

	"github.com/oniharnantyo/buddies/internal/store"
)

// service is the store-backed Service implementation.
type service struct {
	store store.Store
}

func (s *service) CreateSession(ctx context.Context, agentID, title string) (Session, error) {
	session := &store.Session{AgentID: agentID, Title: title}
	if err := s.store.Sessions().Create(ctx, session); err != nil {
		return Session{}, fmt.Errorf("create session: %w", err)
	}
	return *session, nil
}

func (s *service) Session(ctx context.Context, id string) (Session, error) {
	session, err := s.store.Sessions().ByID(ctx, id)
	if err != nil {
		return Session{}, fmt.Errorf("load session: %w", err)
	}
	return *session, nil
}

func (s *service) SessionsByAgent(ctx context.Context, agentID string) ([]Session, error) {
	sessions, err := s.store.Sessions().ListByAgent(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	return sessions, nil
}

func (s *service) AllSessions(ctx context.Context) ([]Session, error) {
	sessions, err := s.store.Sessions().List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	return sessions, nil
}

func (s *service) SetTitle(ctx context.Context, id, title string) error {
	if err := s.store.Sessions().SetTitle(ctx, id, title); err != nil {
		return fmt.Errorf("set session title: %w", err)
	}
	return nil
}

func (s *service) SetOverrides(ctx context.Context, id, model, effort string) error {
	if err := s.store.Sessions().SetOverrides(ctx, id, model, effort); err != nil {
		return fmt.Errorf("set session overrides: %w", err)
	}
	return nil
}

// Delete removes the session, its event log, and its compaction checkpoint
// atomically. An unknown session is a safe no-op: the deletes of absent rows
// succeed and the absent checkpoint's ErrNotFound is absorbed (spec
// chat-view: delete removes the session and its transcript — nothing to
// remove is nothing to do). Interrupt checkpoints of the session's past
// turns are keyed by turn id and simply orphan; a deleted session can never
// resume them.
func (s *service) Delete(ctx context.Context, id string) error {
	err := s.store.WithTx(ctx, func(tx store.Store) error {
		if err := tx.SessionEvents().DeleteAll(ctx, id); err != nil {
			return err
		}
		if err := tx.Sessions().Delete(ctx, id); err != nil {
			return err
		}
		if err := tx.Checkpoints().Delete(ctx, CheckpointID(id)); err != nil && !errors.Is(err, store.ErrNotFound) {
			return err
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}
