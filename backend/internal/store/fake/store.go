// Package fake is an in-memory implementation of store.Store, used by
// handler and pager tests so they can run without any real AWS dependency.
// It mimics dynamo's conditional-write semantics closely enough to exercise
// the same code paths (idempotent create, conflicting ack, etc).
package fake

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/dgarwin/alertme/backend/internal/model"
	"github.com/dgarwin/alertme/backend/internal/store"
)

type Store struct {
	mu sync.Mutex

	users    map[string]model.User
	devices  map[string]map[string]model.Device  // userID -> token -> device
	pairings map[string]map[string]model.Pairing // userID -> peerID -> pairing
	invites  map[string]model.Invite
	pages    map[string]model.Page
	events   map[string][]model.PageEvent
	idem     map[string]string // idempotency key -> page id
}

var (
	_ store.Store                = (*Store)(nil)
	_ store.IdempotentPageLookup = (*Store)(nil)
	_ store.DeviceDeleter        = (*Store)(nil)
)

func New() *Store {
	return &Store{
		users:    make(map[string]model.User),
		devices:  make(map[string]map[string]model.Device),
		pairings: make(map[string]map[string]model.Pairing),
		invites:  make(map[string]model.Invite),
		pages:    make(map[string]model.Page),
		events:   make(map[string][]model.PageEvent),
		idem:     make(map[string]string),
	}
}

func (s *Store) PutUser(ctx context.Context, u model.User) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.users[u.ID] = u
	return nil
}

func (s *Store) GetUser(ctx context.Context, id string) (model.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[id]
	if !ok {
		return model.User{}, store.ErrNotFound
	}
	return u, nil
}

func (s *Store) PutDevice(ctx context.Context, d model.Device) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.devices[d.UserID] == nil {
		s.devices[d.UserID] = make(map[string]model.Device)
	}
	s.devices[d.UserID][d.Token] = d
	return nil
}

func (s *Store) ListDevices(ctx context.Context, userID string) ([]model.Device, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []model.Device
	for _, d := range s.devices[userID] {
		out = append(out, d)
	}
	return out, nil
}

func (s *Store) DeleteDevice(ctx context.Context, userID, token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.devices[userID], token)
	return nil
}

func (s *Store) CreateInvite(ctx context.Context, inv model.Invite) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.invites[inv.Code] = inv
	return nil
}

func (s *Store) AcceptInvite(ctx context.Context, code string, accepter model.User) (model.Pairing, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	inv, ok := s.invites[code]
	if !ok || !inv.ExpiresAt.After(time.Now()) {
		return model.Pairing{}, store.ErrNotFound
	}
	creator, ok := s.users[inv.CreatorID]
	if !ok {
		return model.Pairing{}, fmt.Errorf("fake store: invite creator %s has no user row", inv.CreatorID)
	}
	delete(s.invites, code)

	now := time.Now()
	accepterPairing := model.Pairing{UserID: accepter.ID, PeerID: inv.CreatorID, PeerName: creator.DisplayName, Status: model.PairingActive, CreatedAt: now}
	creatorPairing := model.Pairing{UserID: inv.CreatorID, PeerID: accepter.ID, PeerName: accepter.DisplayName, Status: model.PairingActive, CreatedAt: now}

	if s.pairings[accepter.ID] == nil {
		s.pairings[accepter.ID] = make(map[string]model.Pairing)
	}
	if s.pairings[inv.CreatorID] == nil {
		s.pairings[inv.CreatorID] = make(map[string]model.Pairing)
	}
	s.pairings[accepter.ID][inv.CreatorID] = accepterPairing
	s.pairings[inv.CreatorID][accepter.ID] = creatorPairing

	return accepterPairing, nil
}

func (s *Store) GetPairing(ctx context.Context, userID, peerID string) (model.Pairing, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.pairings[userID][peerID]
	if !ok {
		return model.Pairing{}, store.ErrNotFound
	}
	return p, nil
}

func (s *Store) ListPairings(ctx context.Context, userID string) ([]model.Pairing, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []model.Pairing
	for _, p := range s.pairings[userID] {
		out = append(out, p)
	}
	return out, nil
}

func (s *Store) BlockPairing(ctx context.Context, userID, peerID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.pairings[userID][peerID]
	if !ok {
		return store.ErrNotFound
	}
	p.Status = model.PairingBlocked
	s.pairings[userID][peerID] = p
	return nil
}

func (s *Store) CreatePage(ctx context.Context, p model.Page) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.pages[p.ID]; exists {
		return store.ErrConflict
	}
	if _, exists := s.idem[p.IdempotencyKey]; exists {
		return store.ErrConflict
	}
	s.pages[p.ID] = p
	s.idem[p.IdempotencyKey] = p.ID
	s.events[p.ID] = append(s.events[p.ID], model.PageEvent{PageID: p.ID, Type: "created", At: p.CreatedAt})
	return nil
}

func (s *Store) GetPageByIdempotencyKey(ctx context.Context, key string) (model.Page, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, ok := s.idem[key]
	if !ok {
		return model.Page{}, store.ErrNotFound
	}
	p, ok := s.pages[id]
	if !ok {
		return model.Page{}, store.ErrNotFound
	}
	return p, nil
}

func (s *Store) GetPage(ctx context.Context, id string) (model.Page, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.pages[id]
	if !ok {
		return model.Page{}, store.ErrNotFound
	}
	return p, nil
}

func (s *Store) AckPage(ctx context.Context, id, note string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.pages[id]
	if !ok {
		return store.ErrNotFound
	}
	if !p.State.Active() {
		return store.ErrConflict
	}
	p.State = model.PageAcked
	ackedAt := at
	p.AckedAt = &ackedAt
	p.AckNote = note
	s.pages[id] = p
	return nil
}

func (s *Store) SetPageState(ctx context.Context, id string, from []model.PageState, to model.PageState, attempt int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.pages[id]
	if !ok {
		return store.ErrNotFound
	}
	allowed := false
	for _, st := range from {
		if p.State == st {
			allowed = true
			break
		}
	}
	if !allowed {
		return store.ErrConflict
	}
	p.State = to
	p.Attempt = attempt
	s.pages[id] = p
	return nil
}

func (s *Store) AppendEvent(ctx context.Context, e model.PageEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events[e.PageID] = append(s.events[e.PageID], e)
	return nil
}

func (s *Store) ListEvents(ctx context.Context, pageID string) ([]model.PageEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]model.PageEvent, len(s.events[pageID]))
	copy(out, s.events[pageID])
	return out, nil
}

func (s *Store) ListPagesFor(ctx context.Context, userID string, since time.Time) ([]model.Page, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []model.Page
	for _, p := range s.pages {
		if p.SenderID != userID && p.RecipientID != userID {
			continue
		}
		if !since.IsZero() && !p.CreatedAt.After(since) {
			continue
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	if len(out) > 100 {
		out = out[:100]
	}
	return out, nil
}
