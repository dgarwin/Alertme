// Package api is the whole REST surface — one chi router served by the api
// lambda. Authentication happens BEFORE this code runs (API Gateway JWT
// authorizer); middleware here only extracts the verified claims.
package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/dgarwin/alertme/backend/internal/queue"
	"github.com/dgarwin/alertme/backend/internal/store"
)

type Server struct {
	store store.Store
	queue queue.Enqueuer
}

func New(st store.Store, q queue.Enqueuer) *Server {
	return &Server{store: st, queue: q}
}

// Router wires the complete v1 API (8 routes + health).
func (s *Server) Router() http.Handler {
	r := chi.NewRouter()

	r.Get("/health", s.handleHealth)

	r.Group(func(r chi.Router) {
		r.Use(withIdentity) // Cognito claims → request context

		r.Post("/devices", s.handleRegisterDevice)

		r.Post("/invites", s.handleCreateInvite)
		r.Post("/invites/{code}/accept", s.handleAcceptInvite)
		r.Get("/pairings", s.handleListPairings)
		r.Post("/pairings/{peerID}/block", s.handleBlockPairing)

		r.Post("/pages", s.handleCreatePage)
		r.Get("/pages", s.handleListPages)
		r.Get("/pages/{id}", s.handleGetPage)
		r.Post("/pages/{id}/ack", s.handleAckPage)
		r.Post("/pages/{id}/delivered", s.handleDeliveredReceipt)
	})

	return r
}
