package manager

// SessionHandle identifies a live downstream session so it can be dropped (for a
// role change — D-9 — or to replace a stale duplicate — Q6).
type SessionHandle struct {
	cancel func()
}

// RegisterSession records a live session for id and returns its handle. If a session
// for id already exists it is cancelled first (replace stale duplicate, Q6).
func (m *Manager) RegisterSession(id string, cancel func()) *SessionHandle {
	h := &SessionHandle{cancel: cancel}
	m.mu.Lock()
	prev := m.sessions[id]
	m.sessions[id] = h
	m.mu.Unlock()
	if prev != nil {
		prev.cancel()
	}
	return h
}

// UnregisterSession removes h from the registry, but only if it is still the current
// handle for id (so a replaced session doesn't evict its successor).
func (m *Manager) UnregisterSession(id string, h *SessionHandle) {
	m.mu.Lock()
	if m.sessions[id] == h {
		delete(m.sessions, id)
	}
	m.mu.Unlock()
}

// DropSession cancels the live session for id, if any.
func (m *Manager) DropSession(id string) {
	m.mu.Lock()
	h := m.sessions[id]
	m.mu.Unlock()
	if h != nil {
		h.cancel()
	}
}
