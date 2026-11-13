package dnsprovider

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Memory is an in-memory DNS host for tests: zones, records, scoped access
// and injectable failures. It is not used outside tests.
type Memory struct {
	mu      sync.Mutex
	Token   string // the only accepted token ("" accepts any)
	Expires time.Time
	Revoked bool
	zones   map[string]*memZone
	seq     int
	Calls   map[string]int
	// Hooks for fault injection. A hook runs before the operation; a non-nil
	// error is returned instead of performing it.
	FailOn func(op string, r Record) error
	// AfterCreate runs after a create succeeded but before it returns, to
	// simulate a lost response (return an error from the hook).
	AfterCreate func(r Record) error
	// BeforeCreate runs inside Create, before the duplicate check, with a
	// function that adds a record to the zone: it simulates another
	// administrator creating the same record a moment earlier.
	BeforeCreate func(add func(Record) Record, r Record)
}

type memZone struct {
	Zone
	recs   []Record
	dnssec string
}

func NewMemory() *Memory { return &Memory{zones: map[string]*memZone{}, Calls: map[string]int{}} }

// AddZone registers a zone and returns it.
func (m *Memory) AddZone(id, name string, ns ...string) Zone {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(ns) == 0 {
		ns = []string{"ada.ns.cloudflare.com", "bob.ns.cloudflare.com"}
	}
	z := &memZone{Zone: Zone{ID: id, Name: name, NameServers: ns, Status: "active"}, dnssec: "disabled"}
	m.zones[id] = z
	return z.Zone
}

func (m *Memory) SetDNSSEC(zoneID, status string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.zones[zoneID].dnssec = status
}

// Seed adds a record as if it already existed in the zone.
func (m *Memory) Seed(zoneID string, r Record) Record {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.add(zoneID, r)
}

func (m *Memory) add(zoneID string, r Record) Record {
	m.seq++
	r.ID = fmt.Sprintf("rec%d", m.seq)
	r.Name = strings.ToLower(r.Name)
	if r.TTL == 0 {
		r.TTL = 1
	}
	r.Modified = fmt.Sprintf("v%d", m.seq)
	m.zones[zoneID].recs = append(m.zones[zoneID].recs, r)
	return r
}

// Snapshot returns a copy of the zone's records.
func (m *Memory) Snapshot(zoneID string) []Record {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Record(nil), m.zones[zoneID].recs...)
}

// Edit changes a record as another administrator would (content and stamp).
func (m *Memory) Edit(zoneID, id, content string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	z := m.zones[zoneID]
	for i := range z.recs {
		if z.recs[i].ID == id {
			m.seq++
			z.recs[i].Content = content
			z.recs[i].Modified = fmt.Sprintf("v%d", m.seq)
		}
	}
}

// Remove deletes a record as another administrator would.
func (m *Memory) Remove(zoneID, id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	z := m.zones[zoneID]
	for i := range z.recs {
		if z.recs[i].ID == id {
			z.recs = append(z.recs[:i], z.recs[i+1:]...)
			return
		}
	}
}

func (m *Memory) Count(op string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.Calls[op]
}

func (m *Memory) Name() string { return "memory" }

func (m *Memory) enter(op string, r Record) error {
	m.Calls[op]++
	if m.Revoked {
		return ErrUnauthorized
	}
	if !m.Expires.IsZero() && time.Now().After(m.Expires) {
		return ErrUnauthorized
	}
	if m.FailOn != nil {
		if err := m.FailOn(op, r); err != nil {
			return err
		}
	}
	return nil
}

func (m *Memory) Verify(ctx context.Context) (time.Time, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.enter("verify", Record{}); err != nil {
		return time.Time{}, err
	}
	return m.Expires, nil
}

// Factory returns a Factory that hands out m for token (or any token when
// m.Token is empty) and refuses others.
func (m *Memory) Factory() Factory {
	return func(provider, token string) (Provider, error) {
		if m.Token != "" && token != m.Token {
			return nil, ErrUnauthorized
		}
		return m, nil
	}
}

func (m *Memory) Zones(ctx context.Context) ([]Zone, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.enter("zones", Record{}); err != nil {
		return nil, err
	}
	var out []Zone
	for _, z := range m.zones {
		out = append(out, z.Zone)
	}
	return out, nil
}

func (m *Memory) Zone(ctx context.Context, id string) (Zone, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.enter("zone", Record{}); err != nil {
		return Zone{}, err
	}
	z, ok := m.zones[id]
	if !ok {
		return Zone{}, ErrForbidden // a scoped token cannot see other zones
	}
	return z.Zone, nil
}

func (m *Memory) Records(ctx context.Context, id string) ([]Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.enter("records", Record{}); err != nil {
		return nil, err
	}
	z, ok := m.zones[id]
	if !ok {
		return nil, ErrForbidden
	}
	return append([]Record(nil), z.recs...), nil
}

func (m *Memory) DNSSEC(ctx context.Context, id string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.enter("dnssec", Record{}); err != nil {
		return "", err
	}
	z, ok := m.zones[id]
	if !ok {
		return "", ErrForbidden
	}
	return z.dnssec, nil
}

func (m *Memory) Create(ctx context.Context, id string, r Record) (Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.enter("create", r); err != nil {
		return Record{}, err
	}
	z, ok := m.zones[id]
	if !ok {
		return Record{}, ErrForbidden
	}
	if m.BeforeCreate != nil {
		m.BeforeCreate(func(x Record) Record { return m.add(id, x) }, r)
	}
	for _, e := range z.recs {
		if e.Type == r.Type && e.Name == strings.ToLower(r.Name) && e.Content == r.Content {
			return Record{}, ErrConflict
		}
	}
	out := m.add(id, r)
	if m.AfterCreate != nil {
		if err := m.AfterCreate(out); err != nil {
			return Record{}, err
		}
	}
	return out, nil
}

func (m *Memory) Update(ctx context.Context, id, rid string, r Record) (Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.enter("update", r); err != nil {
		return Record{}, err
	}
	z, ok := m.zones[id]
	if !ok {
		return Record{}, ErrForbidden
	}
	for i := range z.recs {
		if z.recs[i].ID == rid {
			m.seq++
			z.recs[i].Content = r.Content
			z.recs[i].Modified = fmt.Sprintf("v%d", m.seq)
			return z.recs[i], nil
		}
	}
	return Record{}, ErrNotFound
}

func (m *Memory) Delete(ctx context.Context, id, rid string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.enter("delete", Record{}); err != nil {
		return err
	}
	z, ok := m.zones[id]
	if !ok {
		return ErrForbidden
	}
	for i := range z.recs {
		if z.recs[i].ID == rid {
			z.recs = append(z.recs[:i], z.recs[i+1:]...)
			return nil
		}
	}
	return ErrNotFound
}
