package router

import (
	"net/http"
	"strings"
	"testing"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
)

func TestGeneratedRoutes(t *testing.T) {
	h := server.New()
	GeneratedRegister(h)

	tests := []struct {
		method string
		path   string
		status int
		body   string
	}{
		{http.MethodGet, "/healthz", http.StatusOK, `{"status":"ok"}`},
		{http.MethodPost, "/graphs", http.StatusBadRequest, ""},
		{http.MethodGet, "/graphs/g1", http.StatusBadRequest, ""},
		{http.MethodPost, "/graphs/g1/facts", http.StatusBadRequest, ""},
		{http.MethodPost, "/graphs/g1/fact-changes", http.StatusBadRequest, ""},
		{http.MethodGet, "/graphs/g1/nodes/n1", http.StatusBadRequest, ""},
		{http.MethodPost, "/graphs/g1/nodes/n1/evaluations", http.StatusBadRequest, ""},
		{http.MethodPost, "/graphs/g1/facts:search", http.StatusBadRequest, ""},
		{http.MethodPost, "/graphs/g1/relations:trace", http.StatusBadRequest, ""},
		{http.MethodGet, "/sync-events/e1", http.StatusBadRequest, ""},
		{http.MethodGet, "/changes/c1/sync-events", http.StatusBadRequest, ""},
		{http.MethodPost, "/graphs/g1/facts:wrong", http.StatusNotFound, ""},
		{http.MethodPost, "/graphs/g1/factssearch", http.StatusNotFound, ""},
		{http.MethodPost, "/graphs/g1/relations:wrong", http.StatusNotFound, ""},
		{http.MethodPost, "/graphs/g1/relationstrace", http.StatusNotFound, ""},
	}
	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			got := ut.PerformRequest(h.Engine, tt.method, tt.path, nil)
			if got.Code != tt.status {
				t.Errorf("status = %d, want %d", got.Code, tt.status)
			}
			if tt.body != "" && got.Body.String() != tt.body {
				t.Errorf("body = %q, want %q", got.Body.String(), tt.body)
			}
		})
	}
}

func TestAddFactBindsFlatProtobufJSON(t *testing.T) {
	h := server.New()
	GeneratedRegister(h)
	path := "/graphs/019aab23-8765-7123-8123-123456789abc/facts"
	body := `{"user_id":"owner","expected_graph_version":"1","fact":{"content":"office","reason":"confirmed","valid_from":"2025-01-01T00:00:00Z"}}`
	got := ut.PerformRequest(h.Engine, http.MethodPost, path, &ut.Body{Body: strings.NewReader(body), Len: len(body)})
	if got.Code != http.StatusServiceUnavailable || !strings.Contains(got.Body.String(), "MySQL is unavailable") {
		t.Fatalf("valid request did not reach the database lookup: status %d, body %s", got.Code, got.Body.String())
	}
}
