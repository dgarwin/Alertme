package api

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/dgarwin/alertme/backend/internal/model"
	"github.com/dgarwin/alertme/backend/internal/nag"
	"github.com/dgarwin/alertme/backend/internal/queue"
	"github.com/dgarwin/alertme/backend/internal/store"
)

// Handler contracts (request/response bodies) are fixed here. Error mapping
// convention: store.ErrNotFound→404, store.ErrConflict→409 (or idempotent
// 200 where the comment says so), validation→400, everything else→500+log.

// maxMessageLen bounds POST /pages message bodies.
const maxMessageLen = 500

// inviteValidity is how long a created invite code stays acceptable. Not
// specified in the route contract; a week is a reasonable default for a
// link shared out-of-band (text message, etc.).
const inviteValidity = 7 * 24 * time.Hour

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ensureUser upserts a user profile row for the caller if one doesn't exist
// yet, deriving a display name from the verified identity. Both device
// registration and invite acceptance need a real user row: the former
// because it's the natural "first thing the app does after sign-in," the
// latter because AcceptInvite needs a display name to denormalize into the
// creator's pairing copy.
func (s *Server) ensureUser(r *http.Request, id Identity) (model.User, error) {
	u, err := s.store.GetUser(r.Context(), id.UserID)
	if err == nil {
		return u, nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return model.User{}, err
	}
	u = model.User{ID: id.UserID, DisplayName: displayNameFor(id), CreatedAt: time.Now()}
	if err := s.store.PutUser(r.Context(), u); err != nil {
		return model.User{}, err
	}
	return u, nil
}

func displayNameFor(id Identity) string {
	if id.Email != "" {
		if at := strings.IndexByte(id.Email, '@'); at > 0 {
			return id.Email[:at]
		}
		return id.Email
	}
	return id.UserID
}

// POST /devices {token, platform} → 204
func (s *Server) handleRegisterDevice(w http.ResponseWriter, r *http.Request) {
	id, ok := CallerFrom(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var body struct {
		Token    string `json:"token"`
		Platform string `json:"platform"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	if body.Token == "" || (body.Platform != "ios" && body.Platform != "android") {
		http.Error(w, "token and platform (ios|android) are required", http.StatusBadRequest)
		return
	}

	if _, err := s.ensureUser(r, id); err != nil {
		mapStoreErr(w, err)
		return
	}

	dev := model.Device{UserID: id.UserID, Token: body.Token, Platform: body.Platform, UpdatedAt: time.Now()}
	if err := s.store.PutDevice(r.Context(), dev); err != nil {
		mapStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// POST /invites {} → 201 {code, expires_at}   (code: 10 chars, crockford32)
func (s *Server) handleCreateInvite(w http.ResponseWriter, r *http.Request) {
	id, ok := CallerFrom(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	inv := model.Invite{
		Code:      randomInviteCode(10),
		CreatorID: id.UserID,
		ExpiresAt: time.Now().Add(inviteValidity),
	}
	if err := s.store.CreateInvite(r.Context(), inv); err != nil {
		mapStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"code":       inv.Code,
		"expires_at": inv.ExpiresAt,
	})
}

// POST /invites/{code}/accept → 200 {pairing}  (consumes invite atomically;
// pairing with yourself → 400; expired/used code → 404)
func (s *Server) handleAcceptInvite(w http.ResponseWriter, r *http.Request) {
	id, ok := CallerFrom(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	code := chi.URLParam(r, "code")

	accepter, err := s.ensureUser(r, id)
	if err != nil {
		mapStoreErr(w, err)
		return
	}

	pairing, err := s.store.AcceptInvite(r.Context(), code, accepter)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrConflict) {
			http.Error(w, "invite not found or expired", http.StatusNotFound)
			return
		}
		mapStoreErr(w, err)
		return
	}
	// The store has no way to reject a self-accept before consuming the
	// invite (Store doesn't expose a peek-without-consuming lookup), so the
	// check happens here, after the write. A self-accept still leaves a
	// harmless self-referential pairing row, but the caller sees the 400
	// the contract asks for and shouldn't treat it as a real pairing.
	if pairing.PeerID == id.UserID {
		http.Error(w, "cannot pair with yourself", http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"pairing": pairing})
}

// GET /pairings → 200 {pairings: []}
func (s *Server) handleListPairings(w http.ResponseWriter, r *http.Request) {
	id, ok := CallerFrom(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	pairings, err := s.store.ListPairings(r.Context(), id.UserID)
	if err != nil {
		mapStoreErr(w, err)
		return
	}
	if pairings == nil {
		pairings = []model.Pairing{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"pairings": pairings})
}

// POST /pairings/{peerID}/block → 204  (silent to the blocked side)
func (s *Server) handleBlockPairing(w http.ResponseWriter, r *http.Request) {
	id, ok := CallerFrom(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	peerID := chi.URLParam(r, "peerID")
	if err := s.store.BlockPairing(r.Context(), id.UserID, peerID); err != nil {
		mapStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// POST /pages {recipient_id, message, idempotency_key} → 201 {page}
// Must: verify an ACTIVE pairing sender↔recipient (blocked ⇒ 403), create the
// page transactionally (replayed idempotency_key ⇒ 200 with the original
// page), then enqueue queue.PageTask{Attempt: 0} with zero delay.
func (s *Server) handleCreatePage(w http.ResponseWriter, r *http.Request) {
	id, ok := CallerFrom(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var body struct {
		RecipientID    string `json:"recipient_id"`
		Message        string `json:"message"`
		IdempotencyKey string `json:"idempotency_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	if body.RecipientID == "" || body.Message == "" || body.IdempotencyKey == "" {
		http.Error(w, "recipient_id, message, and idempotency_key are required", http.StatusBadRequest)
		return
	}
	if len(body.Message) > maxMessageLen {
		http.Error(w, "message exceeds 500 characters", http.StatusBadRequest)
		return
	}

	pairing, err := s.store.GetPairing(r.Context(), id.UserID, body.RecipientID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, "no active pairing with recipient", http.StatusForbidden)
			return
		}
		mapStoreErr(w, err)
		return
	}
	if pairing.Status != model.PairingActive {
		http.Error(w, "pairing is blocked", http.StatusForbidden)
		return
	}

	now := time.Now()
	page := model.Page{
		ID:          randomID(),
		SenderID:    id.UserID,
		RecipientID: body.RecipientID,
		Message:     body.Message,
		State:       model.PageCreated,
		// -1, not 0: the pager drops a task when page.Attempt >= task.Attempt
		// (an at-least-once SQS duplicate of an attempt already recorded).
		// Attempt 0 is the *first* push, so a freshly created page — no
		// push sent yet — must start below it, or its first nag task would
		// look like a duplicate of itself.
		Attempt:        -1,
		IdempotencyKey: body.IdempotencyKey,
		CreatedAt:      now,
		ExpiresAt:      now.Add(nag.Expiry),
	}

	err = s.store.CreatePage(r.Context(), page)
	if err != nil {
		if errors.Is(err, store.ErrConflict) {
			if lookup, ok := s.store.(store.IdempotentPageLookup); ok {
				original, lookupErr := lookup.GetPageByIdempotencyKey(r.Context(), body.IdempotencyKey)
				if lookupErr == nil {
					writeJSON(w, http.StatusOK, map[string]any{"page": original})
					return
				}
			}
			http.Error(w, "conflict", http.StatusConflict)
			return
		}
		mapStoreErr(w, err)
		return
	}

	if err := s.queue.Enqueue(r.Context(), queue.PageTask{PageID: page.ID, Attempt: 0}, 0); err != nil {
		log.Printf("page %s created but failed to enqueue attempt 0: %v", page.ID, err)
		http.Error(w, "page created but could not be scheduled", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{"page": page})
}

// parseSince parses the ?since= query parameter, defaulting to the zero time
// (no lower bound) when absent.
func parseSince(r *http.Request) (time.Time, error) {
	raw := r.URL.Query().Get("since")
	if raw == "" {
		return time.Time{}, nil
	}
	return time.Parse(time.RFC3339, raw)
}

// GET /pages?since=<rfc3339> → 200 {pages: []}  (poll endpoint; caller must
// be sender or recipient of each page returned)
func (s *Server) handleListPages(w http.ResponseWriter, r *http.Request) {
	id, ok := CallerFrom(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	since, err := parseSince(r)
	if err != nil {
		http.Error(w, "invalid since (want RFC3339)", http.StatusBadRequest)
		return
	}
	pages, err := s.store.ListPagesFor(r.Context(), id.UserID, since)
	if err != nil {
		mapStoreErr(w, err)
		return
	}
	if pages == nil {
		pages = []model.Page{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"pages": pages})
}

// GET /pages/{id} → 200 {page, events: []}  (sender or recipient only)
func (s *Server) handleGetPage(w http.ResponseWriter, r *http.Request) {
	id, ok := CallerFrom(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	pageID := chi.URLParam(r, "id")

	page, err := s.store.GetPage(r.Context(), pageID)
	if err != nil {
		mapStoreErr(w, err)
		return
	}
	if page.SenderID != id.UserID && page.RecipientID != id.UserID {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	events, err := s.store.ListEvents(r.Context(), pageID)
	if err != nil {
		mapStoreErr(w, err)
		return
	}
	if events == nil {
		events = []model.PageEvent{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"page": page, "events": events})
}

// POST /pages/{id}/ack {note?} → 200 {page}  (recipient only; idempotent —
// a second ack returns the already-acked page, not an error)
func (s *Server) handleAckPage(w http.ResponseWriter, r *http.Request) {
	id, ok := CallerFrom(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	pageID := chi.URLParam(r, "id")

	page, err := s.store.GetPage(r.Context(), pageID)
	if err != nil {
		mapStoreErr(w, err)
		return
	}
	if page.RecipientID != id.UserID {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	var body struct {
		Note string `json:"note"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&body) // note is optional; ignore malformed/empty body
	}

	now := time.Now()
	err = s.store.AckPage(r.Context(), pageID, body.Note, now)
	if err != nil {
		if errors.Is(err, store.ErrConflict) {
			current, getErr := s.store.GetPage(r.Context(), pageID)
			if getErr == nil && current.State == model.PageAcked {
				writeJSON(w, http.StatusOK, map[string]any{"page": current})
				return
			}
			http.Error(w, "conflict", http.StatusConflict)
			return
		}
		mapStoreErr(w, err)
		return
	}

	if err := s.store.AppendEvent(r.Context(), model.PageEvent{PageID: pageID, Type: "acked", Detail: body.Note, At: now}); err != nil {
		log.Printf("page %s acked but failed to append event: %v", pageID, err)
	}

	page.State = model.PageAcked
	page.AckedAt = &now
	page.AckNote = body.Note
	writeJSON(w, http.StatusOK, map[string]any{"page": page})
}

// POST /pages/{id}/delivered → 204  (recipient's notification handler;
// advances created/pushed→delivered, never regresses later states)
func (s *Server) handleDeliveredReceipt(w http.ResponseWriter, r *http.Request) {
	id, ok := CallerFrom(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	pageID := chi.URLParam(r, "id")

	page, err := s.store.GetPage(r.Context(), pageID)
	if err != nil {
		mapStoreErr(w, err)
		return
	}
	if page.RecipientID != id.UserID {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	from := []model.PageState{model.PageCreated, model.PagePushed}
	err = s.store.SetPageState(r.Context(), pageID, from, model.PageDelivered, page.Attempt)
	switch {
	case err == nil:
		if evErr := s.store.AppendEvent(r.Context(), model.PageEvent{PageID: pageID, Type: "delivered", At: time.Now()}); evErr != nil {
			log.Printf("page %s delivered but failed to append event: %v", pageID, evErr)
		}
	case errors.Is(err, store.ErrConflict):
		// Already past "pushed" (delivered/seen/acked/expired/cancelled) —
		// the receipt must never regress state, so a losing conditional
		// update here is a silent no-op, not an error.
	default:
		mapStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// mapStoreErr applies the standard store-error → HTTP-status convention.
func mapStoreErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		http.Error(w, "not found", http.StatusNotFound)
	case errors.Is(err, store.ErrConflict):
		http.Error(w, "conflict", http.StatusConflict)
	default:
		log.Printf("store error: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}
