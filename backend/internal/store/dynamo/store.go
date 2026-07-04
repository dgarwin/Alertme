// Package dynamo implements store.Store on the single-table layout in keys.go.
//
// IMPLEMENTATION NOTES (for the build-out pass):
//   - Use feature/dynamodb/attributevalue for marshalling; embed PK/SK/GSI1*
//     attributes alongside the marshalled model structs.
//   - CreatePage: TransactWriteItems{ Put page (attribute_not_exists(PK)),
//     Put IDEM#<key> (attribute_not_exists(PK)), Put created event }.
//     Map TransactionCanceledException/ConditionalCheckFailed → store.ErrConflict.
//   - AckPage / SetPageState: UpdateItem with a ConditionExpression on the
//     current state being in the allowed `from` set; ConditionalCheckFailed →
//     store.ErrConflict.
//   - AcceptInvite: TransactWriteItems{ Delete invite (attribute_exists),
//     Put PAIR item under accepter, Put PAIR item under creator }.
//   - ListPagesFor: Query GSI1 (recipient side) + Query GSI1 with the sender
//     mirror… see keys.go — merge two queries, sort desc, cap page size.
//   - Set `ttl` (unix seconds) on invite items (ExpiresAt) and on page +
//     event items at CreatedAt + 90 days (retention policy).
package dynamo

import (
	"context"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb"

	"github.com/dgarwin/alertme/backend/internal/model"
	"github.com/dgarwin/alertme/backend/internal/store"
)

type Store struct {
	db    *dynamodb.Client
	table string
	gsi1  string
}

var _ store.Store = (*Store)(nil)

func New(db *dynamodb.Client, table, gsi1 string) *Store {
	return &Store{db: db, table: table, gsi1: gsi1}
}

func (s *Store) PutUser(ctx context.Context, u model.User) error {
	return store.ErrNotImplemented
}

func (s *Store) GetUser(ctx context.Context, id string) (model.User, error) {
	return model.User{}, store.ErrNotImplemented
}

func (s *Store) PutDevice(ctx context.Context, d model.Device) error {
	return store.ErrNotImplemented
}

func (s *Store) ListDevices(ctx context.Context, userID string) ([]model.Device, error) {
	return nil, store.ErrNotImplemented
}

func (s *Store) CreateInvite(ctx context.Context, inv model.Invite) error {
	return store.ErrNotImplemented
}

func (s *Store) AcceptInvite(ctx context.Context, code string, accepter model.User) (model.Pairing, error) {
	return model.Pairing{}, store.ErrNotImplemented
}

func (s *Store) GetPairing(ctx context.Context, userID, peerID string) (model.Pairing, error) {
	return model.Pairing{}, store.ErrNotImplemented
}

func (s *Store) ListPairings(ctx context.Context, userID string) ([]model.Pairing, error) {
	return nil, store.ErrNotImplemented
}

func (s *Store) BlockPairing(ctx context.Context, userID, peerID string) error {
	return store.ErrNotImplemented
}

func (s *Store) CreatePage(ctx context.Context, p model.Page) error {
	return store.ErrNotImplemented
}

func (s *Store) GetPage(ctx context.Context, id string) (model.Page, error) {
	return model.Page{}, store.ErrNotImplemented
}

func (s *Store) AckPage(ctx context.Context, id, note string, at time.Time) error {
	return store.ErrNotImplemented
}

func (s *Store) SetPageState(ctx context.Context, id string, from []model.PageState, to model.PageState, attempt int) error {
	return store.ErrNotImplemented
}

func (s *Store) AppendEvent(ctx context.Context, e model.PageEvent) error {
	return store.ErrNotImplemented
}

func (s *Store) ListEvents(ctx context.Context, pageID string) ([]model.PageEvent, error) {
	return nil, store.ErrNotImplemented
}

func (s *Store) ListPagesFor(ctx context.Context, userID string, since time.Time) ([]model.Page, error) {
	return nil, store.ErrNotImplemented
}
