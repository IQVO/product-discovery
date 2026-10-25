package http

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

type statusBody struct {
	Status string `json:"status"`
}

// problemDetail is the RFC 9457 application/problem+json body.
type problemDetail struct {
	Type     string `json:"type"`
	Title    string `json:"title"`
	Status   int    `json:"status"`
	Detail   string `json:"detail"`
	Instance string `json:"instance"`
}

// writeJSON encodes v as the response body with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError writes err as its RFC 9457 problem. An unmapped error is a 500
// whose detail never leaks the internal message (it is logged instead).
func writeError(w http.ResponseWriter, r *http.Request, err error) {
	p := problemFor(err)
	detail := err.Error()
	if p == internalProblem {
		slog.ErrorContext(r.Context(), "request failed", "error", err, "method", r.Method, "path", r.URL.Path)
		detail = "an unexpected error occurred"
	}
	writeBody(w, p.status, problemBody(r, p, detail))
}

// writeProblem writes p with an explicit detail.
func writeProblem(w http.ResponseWriter, r *http.Request, p problem, detail string) {
	writeBody(w, p.status, problemBody(r, p, detail))
}

func problemBody(r *http.Request, p problem, detail string) problemDetail {
	return problemDetail{Type: problemBase + p.slug, Title: p.title, Status: p.status, Detail: detail, Instance: r.URL.EscapedPath()}
}

func writeBody(w http.ResponseWriter, status int, body problemDetail) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
