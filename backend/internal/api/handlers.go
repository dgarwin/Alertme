package api

import (
	"encoding/json"
	"net/http"
)

// Handler contracts (request/response bodies) are fixed here; bodies marked
// TODO(build-out) return 501 until implemented. Error mapping convention:
// store.ErrNotFound→404, store.ErrConflict→409 (or idempotent 200 where the
// comment says so), validation→400, everything else→500 + log.

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// POST /devices {token, platform} → 204
func (s *Server) handleRegisterDevice(w http.ResponseWriter, r *http.Request) {
	notImplemented(w)
}

// POST /invites {} → 201 {code, expires_at}   (code: 10 chars, crockford32)
func (s *Server) handleCreateInvite(w http.ResponseWriter, r *http.Request) {
	notImplemented(w)
}

// POST /invites/{code}/accept → 200 {pairing}  (consumes invite atomically;
// pairing with yourself → 400; expired/used code → 404)
func (s *Server) handleAcceptInvite(w http.ResponseWriter, r *http.Request) {
	notImplemented(w)
}

// GET /pairings → 200 {pairings: []}
func (s *Server) handleListPairings(w http.ResponseWriter, r *http.Request) {
	notImplemented(w)
}

// POST /pairings/{peerID}/block → 204  (silent to the blocked side)
func (s *Server) handleBlockPairing(w http.ResponseWriter, r *http.Request) {
	notImplemented(w)
}

// POST /pages {recipient_id, message, idempotency_key} → 201 {page}
// Must: verify an ACTIVE pairing sender↔recipient (blocked ⇒ 403), create the
// page transactionally (replayed idempotency_key ⇒ 200 with the original
// page), then enqueue queue.PageTask{Attempt: 0} with zero delay.
func (s *Server) handleCreatePage(w http.ResponseWriter, r *http.Request) {
	notImplemented(w)
}

// GET /pages?since=<rfc3339> → 200 {pages: []}  (poll endpoint; caller must
// be sender or recipient of each page returned)
func (s *Server) handleListPages(w http.ResponseWriter, r *http.Request) {
	notImplemented(w)
}

// GET /pages/{id} → 200 {page, events: []}  (sender or recipient only)
func (s *Server) handleGetPage(w http.ResponseWriter, r *http.Request) {
	notImplemented(w)
}

// POST /pages/{id}/ack {note?} → 200 {page}  (recipient only; idempotent —
// a second ack returns the already-acked page, not an error)
func (s *Server) handleAckPage(w http.ResponseWriter, r *http.Request) {
	notImplemented(w)
}

// POST /pages/{id}/delivered → 204  (recipient's notification handler;
// advances created/pushed→delivered, never regresses later states)
func (s *Server) handleDeliveredReceipt(w http.ResponseWriter, r *http.Request) {
	notImplemented(w)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func notImplemented(w http.ResponseWriter) {
	http.Error(w, "not implemented", http.StatusNotImplemented)
}
