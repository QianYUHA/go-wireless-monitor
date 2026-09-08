package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go-wireless-monitor/internal/alert"
	"go-wireless-monitor/internal/model"
	"go-wireless-monitor/internal/monitor"
)

func TestHealth(t *testing.T) {
	router := newRouter(t)
	response := performRequest(router, http.MethodGet, "/health")
	if response.Code != http.StatusOK {
		t.Fatalf("GET /health status = %d, want %d", response.Code, http.StatusOK)
	}
	assertJSONContentType(t, response)

	var body map[string]string
	decodeJSON(t, response, &body)
	if body["status"] != "ok" {
		t.Errorf("status = %q, want ok", body["status"])
	}
}

func TestAPEndpoints(t *testing.T) {
	router := newRouter(t)

	list := performRequest(router, http.MethodGet, "/aps")
	if list.Code != http.StatusOK {
		t.Fatalf("GET /aps status = %d, want %d", list.Code, http.StatusOK)
	}
	assertJSONContentType(t, list)
	var states []model.APState
	decodeJSON(t, list, &states)
	if len(states) != 2 {
		t.Fatalf("GET /aps returned %d states, want 2", len(states))
	}

	known := performRequest(router, http.MethodGet, "/aps/ap-01")
	if known.Code != http.StatusOK {
		t.Fatalf("GET /aps/ap-01 status = %d, want %d", known.Code, http.StatusOK)
	}
	assertJSONContentType(t, known)
	var state model.APState
	decodeJSON(t, known, &state)
	if state.AP.ID != "ap-01" || state.Telemetry.APID != "ap-01" {
		t.Errorf("GET /aps/ap-01 state = %+v, want updated ap-01 state", state)
	}

	unknown := performRequest(router, http.MethodGet, "/aps/missing")
	if unknown.Code != http.StatusNotFound {
		t.Errorf("GET /aps/missing status = %d, want %d", unknown.Code, http.StatusNotFound)
	}
}

func TestAlerts(t *testing.T) {
	router := newRouter(t)
	response := performRequest(router, http.MethodGet, "/alerts")
	if response.Code != http.StatusOK {
		t.Fatalf("GET /alerts status = %d, want %d", response.Code, http.StatusOK)
	}
	assertJSONContentType(t, response)

	var alerts []alert.Alert
	decodeJSON(t, response, &alerts)
	if len(alerts) != 1 || alerts[0].Type != alert.AlertWeakSignal {
		t.Errorf("GET /alerts = %+v, want one weak-signal alert", alerts)
	}
}

func TestUnsupportedMethodsReturnMethodNotAllowed(t *testing.T) {
	router := newRouter(t)
	response := performRequest(router, http.MethodPost, "/aps")
	if response.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST /aps status = %d, want %d", response.Code, http.StatusMethodNotAllowed)
	}
}

func newRouter(t *testing.T) http.Handler {
	t.Helper()
	monitor, err := monitor.New([]model.AP{{ID: "ap-01"}, {ID: "ap-02"}})
	if err != nil {
		t.Fatalf("monitor.New() error = %v", err)
	}
	telemetry := model.Telemetry{
		APID:             "ap-01",
		RSSI:             -80,
		LatencyMS:        20,
		PacketLossPct:    1,
		ConnectedClients: 5,
		Timestamp:        time.Date(2026, time.September, 8, 12, 0, 0, 0, time.UTC),
	}
	if err := monitor.Update(telemetry); err != nil {
		t.Fatalf("monitor.Update() error = %v", err)
	}
	history, err := alert.NewHistory(10)
	if err != nil {
		t.Fatalf("alert.NewHistory() error = %v", err)
	}
	if err := history.Record(telemetry); err != nil {
		t.Fatalf("history.Record() error = %v", err)
	}
	return NewHandler(monitor, history).Routes()
}

func performRequest(router http.Handler, method, target string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, target, nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func assertJSONContentType(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()
	if contentType := response.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", contentType)
	}
}

func decodeJSON(t *testing.T, response *httptest.ResponseRecorder, target any) {
	t.Helper()
	if err := json.NewDecoder(response.Body).Decode(target); err != nil {
		t.Fatalf("response JSON decode error = %v", err)
	}
}
