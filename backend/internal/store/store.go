// Package store defines the persistence contract. The rest of the codebase —
// handlers, pager worker, canary — depends only on this interface; DynamoDB
// specifics live in store/dynamo.
package store

import (
	"context"
	"errors"
	"time"

	"github.com/dgarwin/alertme/backend/internal/model"
)

var (
	ErrNotFound = errors.New("not found")
	// ErrConflict signals a failed conditional write: duplicate idempotency
	// key, double-ack, invite already used, etc. Handlers map it to the
	// idempotent-success or 409 path — it is not a 500.
	ErrConflict       = errors.New("conflict")
	ErrNotImplemented = errors.New("not implemented")
)

type Store interface {
	// Users
	PutUser(ctx context.Context, u model.User) error
	GetUser(ctx context.Context, id string) (model.User, error)

	// Devices (upsert keyed by token; a user may have several)
	PutDevice(ctx context.Context, d model.Device) error
	ListDevices(ctx context.Context, userID string) ([]model.Device, error)

	// Invites & pairings. AcceptInvite must atomically: consume the invite
	// (conditional delete) and write BOTH pairing copies in one transaction.
	CreateInvite(ctx context.Context, inv model.Invite) error
	AcceptInvite(ctx context.Context, code string, accepter model.User) (model.Pairing, error)
	GetPairing(ctx context.Context, userID, peerID string) (model.Pairing, error)
	ListPairings(ctx context.Context, userID string) ([]model.Pairing, error)
	BlockPairing(ctx context.Context, userID, peerID string) error

	// Pages. CreatePage must be idempotent on page.IdempotencyKey
	// (attribute_not_exists condition) and append the "created" event in the
	// same transaction; on replay it returns ErrConflict and the handler
	// re-reads the original page.
	CreatePage(ctx context.Context, p model.Page) error
	GetPage(ctx context.Context, id string) (model.Page, error)
	// AckPage conditionally moves state→acked only from an Active() state;
	// a losing duplicate returns ErrConflict.
	AckPage(ctx context.Context, id, note string, at time.Time) error
	// SetPageState is used by the pager for pushed/expired transitions and by
	// delivery receipts; it must never move a page out of acked.
	SetPageState(ctx context.Context, id string, from []model.PageState, to model.PageState, attempt int) error
	AppendEvent(ctx context.Context, e model.PageEvent) error
	ListEvents(ctx context.Context, pageID string) ([]model.PageEvent, error)
	// ListPagesFor returns pages where the user is recipient or sender,
	// newest-first, created after `since` (GSI1 query).
	ListPagesFor(ctx context.Context, userID string, since time.Time) ([]model.Page, error)
}
