// Package api exposes monitor state and alert history through HTTP handlers.
package api

import (
	"bytes"
	"encoding/json"
	"net/http"

	"go-wireless-monitor/internal/alert"
	"go-wireless-monitor/internal/monitor"
)

// Handler holds the dependencies needed by the HTTP API.
type Handler struct {
	monitor *monitor.Monitor
	alerts  *alert.History
}

// NewHandler creates HTTP handlers backed by the supplied monitor and history.
func NewHandler(monitor *monitor.Monitor, alerts *alert.History) *Handler {
	return &Handler{monitor: monitor, alerts: alerts}
}

// Routes returns the API router. Method-qualified patterns require Go 1.22+.
func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", h.handleHealth)
	mux.HandleFunc("GET /aps", h.handleAPs)
	mux.HandleFunc("GET /aps/{id}", h.handleAP)
	mux.HandleFunc("GET /alerts", h.handleAlerts)
	return mux
}

func (h *Handler) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) handleAPs(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, h.monitor.List())
}

func (h *Handler) handleAP(w http.ResponseWriter, r *http.Request) {
	state, found := h.monitor.Get(r.PathValue("id"))
	if !found {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "access point not found"})
		return
	}
	writeJSON(w, http.StatusOK, state)
}

func (h *Handler) handleAlerts(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, h.alerts.List())
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(value); err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body.Bytes())
}
