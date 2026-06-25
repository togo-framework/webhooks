package webhooks

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Service) mountRoutes(r chi.Router) {
	r.Route("/api/webhooks", func(r chi.Router) {
		// Subscriptions
		r.Get("/subscriptions", func(w http.ResponseWriter, req *http.Request) {
			writeJSON(w, http.StatusOK, s.Subscriptions())
		})
		r.Post("/subscriptions", func(w http.ResponseWriter, req *http.Request) {
			var in struct {
				URL    string   `json:"url"`
				Events []string `json:"events"`
				Secret string   `json:"secret"`
			}
			if err := json.NewDecoder(req.Body).Decode(&in); err != nil || in.URL == "" {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "url and events required"})
				return
			}
			writeJSON(w, http.StatusCreated, s.Subscribe(in.URL, in.Events, in.Secret))
		})
		r.Delete("/subscriptions/{id}", func(w http.ResponseWriter, req *http.Request) {
			s.Unsubscribe(chi.URLParam(req, "id"))
			writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		})

		// Send an event
		r.Post("/send", func(w http.ResponseWriter, req *http.Request) {
			var in struct {
				Event string `json:"event"`
				Data  any    `json:"data"`
			}
			if err := json.NewDecoder(req.Body).Decode(&in); err != nil || in.Event == "" {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "event required"})
				return
			}
			_ = s.Send(req.Context(), in.Event, in.Data)
			writeJSON(w, http.StatusAccepted, map[string]bool{"queued": true})
		})

		// Delivery log
		r.Get("/deliveries", func(w http.ResponseWriter, req *http.Request) {
			writeJSON(w, http.StatusOK, s.Deliveries())
		})
	})
}
