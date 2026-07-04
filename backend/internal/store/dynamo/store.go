// Package dynamo implements store.Store on the single-table layout in keys.go.
package dynamo

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"github.com/dgarwin/alertme/backend/internal/model"
	"github.com/dgarwin/alertme/backend/internal/store"
)

type Store struct {
	db    *dynamodb.Client
	table string
	gsi1  string // recipient-side page index
	gsi2  string // sender-side page index
}

var (
	_ store.Store                = (*Store)(nil)
	_ store.IdempotentPageLookup = (*Store)(nil)
	_ store.DeviceDeleter        = (*Store)(nil)
)

func New(db *dynamodb.Client, table, gsi1, gsi2 string) *Store {
	return &Store{db: db, table: table, gsi1: gsi1, gsi2: gsi2}
}

// ---------------------------------------------------------------- helpers

func isConditionFailure(err error) bool {
	var e *types.ConditionalCheckFailedException
	return errors.As(err, &e)
}

func isTransactConditionFailure(err error) bool {
	var e *types.TransactionCanceledException
	if !errors.As(err, &e) {
		return false
	}
	for _, r := range e.CancellationReasons {
		if r.Code != nil && *r.Code == "ConditionalCheckFailed" {
			return true
		}
	}
	return false
}

func pageKey(id string) map[string]types.AttributeValue {
	return map[string]types.AttributeValue{
		"PK": &types.AttributeValueMemberS{Value: pagePK(id)},
		"SK": &types.AttributeValueMemberS{Value: skMeta},
	}
}

// stateInCondition builds a "<attr> IN (:from0, :from1, ...)" condition
// expression fragment plus the matching expression attribute values, used by
// AckPage and SetPageState to restrict the update to an allowed `from` set.
func stateInCondition(attr string, from []model.PageState) (string, map[string]types.AttributeValue) {
	placeholders := make([]string, len(from))
	values := make(map[string]types.AttributeValue, len(from))
	for i, st := range from {
		ph := fmt.Sprintf(":from%d", i)
		placeholders[i] = ph
		values[ph] = &types.AttributeValueMemberS{Value: string(st)}
	}
	return fmt.Sprintf("%s IN (%s)", attr, strings.Join(placeholders, ", ")), values
}

// ------------------------------------------------------------------- users

func (s *Store) PutUser(ctx context.Context, u model.User) error {
	item, err := attributevalue.MarshalMap(newUserRecord(u))
	if err != nil {
		return fmt.Errorf("marshal user %s: %w", u.ID, err)
	}
	if _, err := s.db.PutItem(ctx, &dynamodb.PutItemInput{TableName: aws.String(s.table), Item: item}); err != nil {
		return fmt.Errorf("put user %s: %w", u.ID, err)
	}
	return nil
}

func (s *Store) GetUser(ctx context.Context, id string) (model.User, error) {
	out, err := s.db.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(s.table),
		Key: map[string]types.AttributeValue{
			"PK": &types.AttributeValueMemberS{Value: userPK(id)},
			"SK": &types.AttributeValueMemberS{Value: skProfile},
		},
	})
	if err != nil {
		return model.User{}, fmt.Errorf("get user %s: %w", id, err)
	}
	if out.Item == nil {
		return model.User{}, store.ErrNotFound
	}
	var rec userRecord
	if err := attributevalue.UnmarshalMap(out.Item, &rec); err != nil {
		return model.User{}, fmt.Errorf("unmarshal user %s: %w", id, err)
	}
	return rec.toModel()
}

// ------------------------------------------------------------------ devices

func (s *Store) PutDevice(ctx context.Context, d model.Device) error {
	item, err := attributevalue.MarshalMap(newDeviceRecord(d))
	if err != nil {
		return fmt.Errorf("marshal device %s: %w", d.Token, err)
	}
	if _, err := s.db.PutItem(ctx, &dynamodb.PutItemInput{TableName: aws.String(s.table), Item: item}); err != nil {
		return fmt.Errorf("put device %s: %w", d.Token, err)
	}
	return nil
}

func (s *Store) ListDevices(ctx context.Context, userID string) ([]model.Device, error) {
	out, err := s.db.Query(ctx, &dynamodb.QueryInput{
		TableName:              aws.String(s.table),
		KeyConditionExpression: aws.String("PK = :pk AND begins_with(SK, :prefix)"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":pk":     &types.AttributeValueMemberS{Value: userPK(userID)},
			":prefix": &types.AttributeValueMemberS{Value: "DEVICE#"},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("list devices for %s: %w", userID, err)
	}
	devices := make([]model.Device, 0, len(out.Items))
	for _, item := range out.Items {
		var rec deviceRecord
		if err := attributevalue.UnmarshalMap(item, &rec); err != nil {
			return nil, fmt.Errorf("unmarshal device: %w", err)
		}
		d, err := rec.toModel()
		if err != nil {
			return nil, err
		}
		devices = append(devices, d)
	}
	return devices, nil
}

// DeleteDevice implements store.DeviceDeleter (see its doc comment): the
// pager prunes a device once FCM reports its token is no longer valid.
func (s *Store) DeleteDevice(ctx context.Context, userID, token string) error {
	_, err := s.db.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName: aws.String(s.table),
		Key: map[string]types.AttributeValue{
			"PK": &types.AttributeValueMemberS{Value: userPK(userID)},
			"SK": &types.AttributeValueMemberS{Value: devicSK(token)},
		},
	})
	if err != nil {
		return fmt.Errorf("delete device %s: %w", token, err)
	}
	return nil
}

// ---------------------------------------------------------- invites/pairing

func (s *Store) CreateInvite(ctx context.Context, inv model.Invite) error {
	item, err := attributevalue.MarshalMap(newInviteRecord(inv))
	if err != nil {
		return fmt.Errorf("marshal invite %s: %w", inv.Code, err)
	}
	if _, err := s.db.PutItem(ctx, &dynamodb.PutItemInput{TableName: aws.String(s.table), Item: item}); err != nil {
		return fmt.Errorf("create invite %s: %w", inv.Code, err)
	}
	return nil
}

func (s *Store) getInvite(ctx context.Context, code string) (model.Invite, error) {
	out, err := s.db.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(s.table),
		Key: map[string]types.AttributeValue{
			"PK": &types.AttributeValueMemberS{Value: invitePK(code)},
			"SK": &types.AttributeValueMemberS{Value: skMeta},
		},
	})
	if err != nil {
		return model.Invite{}, fmt.Errorf("get invite %s: %w", code, err)
	}
	if out.Item == nil {
		return model.Invite{}, store.ErrNotFound
	}
	var rec inviteRecord
	if err := attributevalue.UnmarshalMap(out.Item, &rec); err != nil {
		return model.Invite{}, fmt.Errorf("unmarshal invite %s: %w", code, err)
	}
	return rec.toModel()
}

func (s *Store) AcceptInvite(ctx context.Context, code string, accepter model.User) (model.Pairing, error) {
	inv, err := s.getInvite(ctx, code)
	if err != nil {
		return model.Pairing{}, err
	}
	if !inv.ExpiresAt.After(time.Now()) {
		return model.Pairing{}, store.ErrNotFound
	}
	creator, err := s.GetUser(ctx, inv.CreatorID)
	if err != nil {
		return model.Pairing{}, fmt.Errorf("load invite creator %s: %w", inv.CreatorID, err)
	}

	now := time.Now()
	accepterPairing := model.Pairing{UserID: accepter.ID, PeerID: inv.CreatorID, PeerName: creator.DisplayName, Status: model.PairingActive, CreatedAt: now}
	creatorPairing := model.Pairing{UserID: inv.CreatorID, PeerID: accepter.ID, PeerName: accepter.DisplayName, Status: model.PairingActive, CreatedAt: now}

	accepterItem, err := attributevalue.MarshalMap(newPairingRecord(accepterPairing))
	if err != nil {
		return model.Pairing{}, fmt.Errorf("marshal pairing: %w", err)
	}
	creatorItem, err := attributevalue.MarshalMap(newPairingRecord(creatorPairing))
	if err != nil {
		return model.Pairing{}, fmt.Errorf("marshal pairing: %w", err)
	}

	_, err = s.db.TransactWriteItems(ctx, &dynamodb.TransactWriteItemsInput{
		TransactItems: []types.TransactWriteItem{
			{Delete: &types.Delete{
				TableName: aws.String(s.table),
				Key: map[string]types.AttributeValue{
					"PK": &types.AttributeValueMemberS{Value: invitePK(code)},
					"SK": &types.AttributeValueMemberS{Value: skMeta},
				},
				ConditionExpression: aws.String("attribute_exists(PK) AND ttl > :now"),
				ExpressionAttributeValues: map[string]types.AttributeValue{
					":now": &types.AttributeValueMemberN{Value: strconv.FormatInt(now.Unix(), 10)},
				},
			}},
			{Put: &types.Put{TableName: aws.String(s.table), Item: accepterItem}},
			{Put: &types.Put{TableName: aws.String(s.table), Item: creatorItem}},
		},
	})
	if err != nil {
		if isTransactConditionFailure(err) {
			// Raced with another accept or an expiry TTL sweep since the
			// pre-check above; treat it the same as "no such invite".
			return model.Pairing{}, store.ErrNotFound
		}
		return model.Pairing{}, fmt.Errorf("accept invite %s: %w", code, err)
	}
	return accepterPairing, nil
}

func (s *Store) GetPairing(ctx context.Context, userID, peerID string) (model.Pairing, error) {
	out, err := s.db.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(s.table),
		Key: map[string]types.AttributeValue{
			"PK": &types.AttributeValueMemberS{Value: userPK(userID)},
			"SK": &types.AttributeValueMemberS{Value: pairSK(peerID)},
		},
	})
	if err != nil {
		return model.Pairing{}, fmt.Errorf("get pairing %s/%s: %w", userID, peerID, err)
	}
	if out.Item == nil {
		return model.Pairing{}, store.ErrNotFound
	}
	var rec pairingRecord
	if err := attributevalue.UnmarshalMap(out.Item, &rec); err != nil {
		return model.Pairing{}, fmt.Errorf("unmarshal pairing: %w", err)
	}
	return rec.toModel()
}

func (s *Store) ListPairings(ctx context.Context, userID string) ([]model.Pairing, error) {
	out, err := s.db.Query(ctx, &dynamodb.QueryInput{
		TableName:              aws.String(s.table),
		KeyConditionExpression: aws.String("PK = :pk AND begins_with(SK, :prefix)"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":pk":     &types.AttributeValueMemberS{Value: userPK(userID)},
			":prefix": &types.AttributeValueMemberS{Value: "PAIR#"},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("list pairings for %s: %w", userID, err)
	}
	pairings := make([]model.Pairing, 0, len(out.Items))
	for _, item := range out.Items {
		var rec pairingRecord
		if err := attributevalue.UnmarshalMap(item, &rec); err != nil {
			return nil, fmt.Errorf("unmarshal pairing: %w", err)
		}
		p, err := rec.toModel()
		if err != nil {
			return nil, err
		}
		pairings = append(pairings, p)
	}
	return pairings, nil
}

func (s *Store) BlockPairing(ctx context.Context, userID, peerID string) error {
	_, err := s.db.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(s.table),
		Key: map[string]types.AttributeValue{
			"PK": &types.AttributeValueMemberS{Value: userPK(userID)},
			"SK": &types.AttributeValueMemberS{Value: pairSK(peerID)},
		},
		UpdateExpression:    aws.String("SET #status = :blocked"),
		ConditionExpression: aws.String("attribute_exists(PK)"),
		ExpressionAttributeNames: map[string]string{
			"#status": "status",
		},
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":blocked": &types.AttributeValueMemberS{Value: string(model.PairingBlocked)},
		},
	})
	if err != nil {
		if isConditionFailure(err) {
			return store.ErrNotFound
		}
		return fmt.Errorf("block pairing %s/%s: %w", userID, peerID, err)
	}
	return nil
}

// -------------------------------------------------------------------- pages

func (s *Store) CreatePage(ctx context.Context, p model.Page) error {
	pageItem, err := attributevalue.MarshalMap(newPageRecord(p))
	if err != nil {
		return fmt.Errorf("marshal page %s: %w", p.ID, err)
	}
	idemItem, err := attributevalue.MarshalMap(idemRecord{PK: idemPK(p.IdempotencyKey), SK: skMeta, PageID: p.ID})
	if err != nil {
		return fmt.Errorf("marshal idempotency guard: %w", err)
	}
	eventItem, err := attributevalue.MarshalMap(newEventRecord(model.PageEvent{PageID: p.ID, Type: "created", At: p.CreatedAt}))
	if err != nil {
		return fmt.Errorf("marshal created event: %w", err)
	}

	_, err = s.db.TransactWriteItems(ctx, &dynamodb.TransactWriteItemsInput{
		TransactItems: []types.TransactWriteItem{
			{Put: &types.Put{
				TableName:           aws.String(s.table),
				Item:                pageItem,
				ConditionExpression: aws.String("attribute_not_exists(PK)"),
			}},
			{Put: &types.Put{
				TableName:           aws.String(s.table),
				Item:                idemItem,
				ConditionExpression: aws.String("attribute_not_exists(PK)"),
			}},
			{Put: &types.Put{
				TableName: aws.String(s.table),
				Item:      eventItem,
			}},
		},
	})
	if err != nil {
		if isTransactConditionFailure(err) {
			return store.ErrConflict
		}
		return fmt.Errorf("create page %s: %w", p.ID, err)
	}
	return nil
}

// GetPageByIdempotencyKey implements store.IdempotentPageLookup: it resolves a
// replayed idempotency key back to the page CreatePage originally created, so
// handleCreatePage can serve the idempotent 200 without a second write.
func (s *Store) GetPageByIdempotencyKey(ctx context.Context, key string) (model.Page, error) {
	out, err := s.db.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(s.table),
		Key: map[string]types.AttributeValue{
			"PK": &types.AttributeValueMemberS{Value: idemPK(key)},
			"SK": &types.AttributeValueMemberS{Value: skMeta},
		},
	})
	if err != nil {
		return model.Page{}, fmt.Errorf("get idempotency guard %s: %w", key, err)
	}
	if out.Item == nil {
		return model.Page{}, store.ErrNotFound
	}
	var rec idemRecord
	if err := attributevalue.UnmarshalMap(out.Item, &rec); err != nil {
		return model.Page{}, fmt.Errorf("unmarshal idempotency guard: %w", err)
	}
	return s.GetPage(ctx, rec.PageID)
}

func (s *Store) GetPage(ctx context.Context, id string) (model.Page, error) {
	out, err := s.db.GetItem(ctx, &dynamodb.GetItemInput{TableName: aws.String(s.table), Key: pageKey(id)})
	if err != nil {
		return model.Page{}, fmt.Errorf("get page %s: %w", id, err)
	}
	if out.Item == nil {
		return model.Page{}, store.ErrNotFound
	}
	var rec pageRecord
	if err := attributevalue.UnmarshalMap(out.Item, &rec); err != nil {
		return model.Page{}, fmt.Errorf("unmarshal page %s: %w", id, err)
	}
	return rec.toModel()
}

func (s *Store) AckPage(ctx context.Context, id, note string, at time.Time) error {
	from := []model.PageState{model.PageCreated, model.PagePushed, model.PageDelivered, model.PageSeen}
	cond, values := stateInCondition("#st", from)
	values[":to"] = &types.AttributeValueMemberS{Value: string(model.PageAcked)}
	values[":at"] = &types.AttributeValueMemberS{Value: fmtTime(at)}
	values[":note"] = &types.AttributeValueMemberS{Value: note}

	_, err := s.db.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName:                 aws.String(s.table),
		Key:                       pageKey(id),
		UpdateExpression:          aws.String("SET #st = :to, acked_at = :at, ack_note = :note"),
		ConditionExpression:       aws.String(cond),
		ExpressionAttributeNames:  map[string]string{"#st": "state"},
		ExpressionAttributeValues: values,
	})
	if err != nil {
		if isConditionFailure(err) {
			return store.ErrConflict
		}
		return fmt.Errorf("ack page %s: %w", id, err)
	}
	return nil
}

func (s *Store) SetPageState(ctx context.Context, id string, from []model.PageState, to model.PageState, attempt int) error {
	cond, values := stateInCondition("#st", from)
	values[":to"] = &types.AttributeValueMemberS{Value: string(to)}
	values[":attempt"] = &types.AttributeValueMemberN{Value: strconv.Itoa(attempt)}

	_, err := s.db.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName:                 aws.String(s.table),
		Key:                       pageKey(id),
		UpdateExpression:          aws.String("SET #st = :to, attempt = :attempt"),
		ConditionExpression:       aws.String(cond),
		ExpressionAttributeNames:  map[string]string{"#st": "state"},
		ExpressionAttributeValues: values,
	})
	if err != nil {
		if isConditionFailure(err) {
			return store.ErrConflict
		}
		return fmt.Errorf("set page %s state to %s: %w", id, to, err)
	}
	return nil
}

func (s *Store) AppendEvent(ctx context.Context, e model.PageEvent) error {
	item, err := attributevalue.MarshalMap(newEventRecord(e))
	if err != nil {
		return fmt.Errorf("marshal event for page %s: %w", e.PageID, err)
	}
	if _, err := s.db.PutItem(ctx, &dynamodb.PutItemInput{TableName: aws.String(s.table), Item: item}); err != nil {
		return fmt.Errorf("append event for page %s: %w", e.PageID, err)
	}
	return nil
}

func (s *Store) ListEvents(ctx context.Context, pageID string) ([]model.PageEvent, error) {
	out, err := s.db.Query(ctx, &dynamodb.QueryInput{
		TableName:              aws.String(s.table),
		KeyConditionExpression: aws.String("PK = :pk AND begins_with(SK, :prefix)"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":pk":     &types.AttributeValueMemberS{Value: pagePK(pageID)},
			":prefix": &types.AttributeValueMemberS{Value: "EVT#"},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("list events for page %s: %w", pageID, err)
	}
	events := make([]model.PageEvent, 0, len(out.Items))
	for _, item := range out.Items {
		var rec eventRecord
		if err := attributevalue.UnmarshalMap(item, &rec); err != nil {
			return nil, fmt.Errorf("unmarshal event: %w", err)
		}
		ev, err := rec.toModel()
		if err != nil {
			return nil, err
		}
		events = append(events, ev)
	}
	return events, nil
}

// queryPageIndex queries one of the page GSIs (recipient or sender side) for
// items created after `since` (or all items, if since is zero), newest first.
func (s *Store) queryPageIndex(ctx context.Context, indexName, pkAttr, skAttr, pkVal string, since time.Time) ([]model.Page, error) {
	keyCond := "#pk = :pk"
	names := map[string]string{"#pk": pkAttr}
	values := map[string]types.AttributeValue{":pk": &types.AttributeValueMemberS{Value: pkVal}}
	if !since.IsZero() {
		keyCond += " AND #sk > :since"
		names["#sk"] = skAttr
		values[":since"] = &types.AttributeValueMemberS{Value: "PAGE#" + since.UTC().Format(time.RFC3339)}
	}

	out, err := s.db.Query(ctx, &dynamodb.QueryInput{
		TableName:                 aws.String(s.table),
		IndexName:                 aws.String(indexName),
		KeyConditionExpression:    aws.String(keyCond),
		ExpressionAttributeNames:  names,
		ExpressionAttributeValues: values,
		ScanIndexForward:          aws.Bool(false),
		Limit:                     aws.Int32(100),
	})
	if err != nil {
		return nil, fmt.Errorf("query %s: %w", indexName, err)
	}
	pages := make([]model.Page, 0, len(out.Items))
	for _, item := range out.Items {
		var rec pageRecord
		if err := attributevalue.UnmarshalMap(item, &rec); err != nil {
			return nil, fmt.Errorf("unmarshal page: %w", err)
		}
		p, err := rec.toModel()
		if err != nil {
			return nil, err
		}
		pages = append(pages, p)
	}
	return pages, nil
}

func (s *Store) ListPagesFor(ctx context.Context, userID string, since time.Time) ([]model.Page, error) {
	pk := userPK(userID)
	asRecipient, err := s.queryPageIndex(ctx, s.gsi1, "GSI1PK", "GSI1SK", pk, since)
	if err != nil {
		return nil, err
	}
	asSender, err := s.queryPageIndex(ctx, s.gsi2, "GSI2PK", "GSI2SK", pk, since)
	if err != nil {
		return nil, err
	}

	byID := make(map[string]model.Page, len(asRecipient)+len(asSender))
	for _, p := range asRecipient {
		byID[p.ID] = p
	}
	for _, p := range asSender {
		byID[p.ID] = p
	}

	merged := make([]model.Page, 0, len(byID))
	for _, p := range byID {
		merged = append(merged, p)
	}
	sort.Slice(merged, func(i, j int) bool { return merged[i].CreatedAt.After(merged[j].CreatedAt) })
	if len(merged) > 100 {
		merged = merged[:100]
	}
	return merged, nil
}
