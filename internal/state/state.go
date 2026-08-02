// Package state holds live, in-memory per-chargepoint status derived from the OCPP
// stream (FR-36/37). It is reset on restart (FR-39) and rendered by the admin
// dashboard (M3).
package state

import (
	"sort"
	"sync"
	"time"
)

// CP is the live state of one chargepoint. Energy is stored normalised to Wh.
type CP struct {
	ID              string
	Role            string // "local" | "proxied"
	DownstreamUp    bool
	UpstreamUp      bool // proxied only
	LastMessage     time.Time
	LastHeartbeat   time.Time
	ConnectorStatus string // latest StatusNotification status
	TxnActive       bool
	TxnID           int
	IDTag           string
	TxnStart        time.Time
	MeterStartWh    float64 // meter register at transaction start
	MeterLastWh     float64 // latest meter register reading
	EnergyWh        float64 // session energy = last − start
	PowerW          float64 // latest sampled active power
	LastStopReason  string
	RemoteStartSent bool // auto-start RemoteStartTransaction issued for the current Preparing episode
	LastConnector   int  // connectorId of the most recent non-zero StatusNotification

	LastScheduleAction time.Time // when the scheduler last issued a start/stop (cooldown guard)
}

// Store is a concurrency-safe collection of CP state.
type Store struct {
	mu sync.RWMutex
	m  map[string]*CP
}

// NewStore creates an empty Store.
func NewStore() *Store {
	return &Store{m: make(map[string]*CP)}
}

// Update applies fn to the CP with the given id under the write lock, creating the
// entry if it does not yet exist.
func (s *Store) Update(id string, fn func(*CP)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := s.m[id]
	if cp == nil {
		cp = &CP{ID: id}
		s.m[id] = cp
	}
	fn(cp)
}

// Get returns a copy of the CP state for id.
func (s *Store) Get(id string) (CP, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cp, ok := s.m[id]
	if !ok {
		return CP{}, false
	}
	return *cp, true
}

// Snapshot returns a copy of all CP states, sorted by ID, for display.
func (s *Store) Snapshot() []CP {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]CP, 0, len(s.m))
	for _, cp := range s.m {
		out = append(out, *cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
