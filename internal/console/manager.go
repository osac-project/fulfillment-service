/*
Copyright (c) 2025 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package console

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
	"time"
)

// ManagerBuilder builds a Manager.
type ManagerBuilder struct {
	logger         *slog.Logger
	backends       map[string]Backend
	sessionTimeout time.Duration
}

// Manager manages console sessions and dispatches to the correct backend.
type Manager struct {
	logger         *slog.Logger
	backends       map[string]Backend
	sessionTimeout time.Duration
	sessions       map[string]*session
	sessionsLock   sync.Mutex
}

type session struct {
	resourceKey string
	user        string
	clientID    string
	startedAt   time.Time
	cancel      context.CancelFunc
}

// NewManager creates a new builder for the console manager.
func NewManager() *ManagerBuilder {
	return &ManagerBuilder{
		backends:       make(map[string]Backend),
		sessionTimeout: defaultSessionTimeout(),
	}
}

func (b *ManagerBuilder) SetLogger(value *slog.Logger) *ManagerBuilder {
	b.logger = value
	return b
}

func (b *ManagerBuilder) SetSessionTimeout(value time.Duration) *ManagerBuilder {
	b.sessionTimeout = value
	return b
}

func (b *ManagerBuilder) AddBackend(resourceType string, backend Backend) *ManagerBuilder {
	b.backends[resourceType] = backend
	return b
}

func (b *ManagerBuilder) Build() (*Manager, error) {
	if b.logger == nil {
		return nil, errors.New("logger is mandatory")
	}
	if len(b.backends) == 0 {
		return nil, errors.New("at least one backend is required")
	}
	return &Manager{
		logger:         b.logger,
		backends:       b.backends,
		sessionTimeout: b.sessionTimeout,
		sessions:       make(map[string]*session),
	}, nil
}

// Connect establishes a console connection to the target resource.
// It returns an io.ReadWriteCloser for bidirectional communication.
// The returned connection is closed when ctx is cancelled or the session times out.
//
// If clientID is non-empty and matches an existing session from the same user,
// the stale session is evicted and the new connection is admitted. This handles
// reconnection after unclean TCP disconnects.
func (m *Manager) Connect(ctx context.Context, target Target, user, clientID string) (io.ReadWriteCloser, error) {
	backend, ok := m.backends[target.ResourceType]
	if !ok {
		return nil, fmt.Errorf("unsupported resource type %q", target.ResourceType)
	}

	consoleType := target.ConsoleType
	if consoleType == "" {
		return nil, fmt.Errorf("console type is required")
	}
	sessionKey := fmt.Sprintf("%s/%s/%s", target.ResourceType, target.ResourceID, consoleType)

	var oldCancel context.CancelFunc

	m.sessionsLock.Lock()
	if existing, ok := m.sessions[sessionKey]; ok {
		if clientID != "" && existing.clientID == clientID && existing.user == user {
			m.logger.InfoContext(ctx, "Evicting stale console session",
				slog.String("resource", sessionKey),
				slog.String("user", user),
				slog.String("client_id", clientID),
				slog.Duration("age", time.Since(existing.startedAt)),
			)
			oldCancel = existing.cancel
			delete(m.sessions, sessionKey)
		} else {
			m.sessionsLock.Unlock()
			return nil, &ErrSessionExists{
				Resource: sessionKey,
				User:     existing.user,
				Since:    existing.startedAt.Format(time.RFC3339),
			}
		}
	}

	// Create session with timeout.
	sessionCtx, sessionCancel := context.WithTimeout(ctx, m.sessionTimeout)
	s := &session{
		resourceKey: sessionKey,
		user:        user,
		clientID:    clientID,
		startedAt:   time.Now(),
		cancel:      sessionCancel,
	}
	m.sessions[sessionKey] = s
	m.sessionsLock.Unlock()

	if oldCancel != nil {
		oldCancel()
	}

	m.logger.InfoContext(ctx, "Opening console session",
		slog.String("resource", sessionKey),
		slog.String("user", user),
		slog.Duration("timeout", m.sessionTimeout),
	)

	conn, err := backend.Connect(sessionCtx, target)
	if err != nil {
		m.removeSession(sessionKey, s)
		sessionCancel()
		return nil, err
	}

	return &managedConnection{
		ReadWriteCloser: conn,
		manager:         m,
		sessionKey:      sessionKey,
		session:         s,
		cancel:          sessionCancel,
	}, nil
}

// ActiveSessions returns the number of active console sessions.
func (m *Manager) ActiveSessions() int {
	m.sessionsLock.Lock()
	defer m.sessionsLock.Unlock()
	return len(m.sessions)
}

// CancelSessions cancels all active session contexts, causing their proxy
// goroutines to shut down asynchronously. It does not wait for the goroutines
// to finish — callers should allow a grace period for in-flight operations
// (e.g., sending disconnect status messages) to complete.
func (m *Manager) CancelSessions() {
	m.sessionsLock.Lock()
	for key, s := range m.sessions {
		m.logger.Info("Cancelling console session",
			slog.String("resource", key),
			slog.String("user", s.user),
		)
		s.cancel()
	}
	m.sessionsLock.Unlock()
}

func (m *Manager) removeSession(key string, owner *session) {
	m.sessionsLock.Lock()
	defer m.sessionsLock.Unlock()
	if m.sessions[key] == owner {
		delete(m.sessions, key)
	}
}

// managedConnection wraps an io.ReadWriteCloser and removes the session on close.
type managedConnection struct {
	io.ReadWriteCloser
	manager    *Manager
	sessionKey string
	session    *session
	cancel     context.CancelFunc
	closeOnce  sync.Once
}

func (c *managedConnection) Close() error {
	var err error
	c.closeOnce.Do(func() {
		c.manager.logger.Info("Closing console session",
			slog.String("resource", c.sessionKey),
		)
		err = c.ReadWriteCloser.Close()
		c.manager.removeSession(c.sessionKey, c.session)
		c.cancel()
	})
	return err
}

func defaultSessionTimeout() time.Duration {
	if v := os.Getenv("OSAC_CONSOLE_SESSION_TIMEOUT"); v != "" {
		d, err := time.ParseDuration(v)
		if err == nil {
			return d
		}
	}
	return 30 * time.Minute
}
