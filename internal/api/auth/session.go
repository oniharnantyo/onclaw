package auth

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

const SessionCookieName = "onclaw_session"
const SessionDuration = 24 * time.Hour

type SessionStore struct {
	mu       sync.RWMutex
	sessions map[string]time.Time
}

func NewSessionStore() *SessionStore {
	return &SessionStore{
		sessions: make(map[string]time.Time),
	}
}

func (s *SessionStore) Create() (string, time.Time, error) {
	tokenBytes := make([]byte, 16)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", time.Time{}, err
	}
	token := hex.EncodeToString(tokenBytes)
	expiry := time.Now().Add(SessionDuration)

	s.mu.Lock()
	s.sessions[token] = expiry
	s.mu.Unlock()

	return token, expiry, nil
}

func (s *SessionStore) Verify(token string) (bool, bool) {
	s.mu.RLock()
	expiry, ok := s.sessions[token]
	s.mu.RUnlock()

	if !ok {
		return false, false
	}
	if time.Now().After(expiry) {
		s.mu.Lock()
		delete(s.sessions, token)
		s.mu.Unlock()
		return false, true // expired
	}
	return true, false // valid
}

func (s *SessionStore) Delete(token string) {
	s.mu.Lock()
	delete(s.sessions, token)
	s.mu.Unlock()
}
