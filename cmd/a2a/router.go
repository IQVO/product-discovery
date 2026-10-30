package main

import (
	"net/http"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"

	inbounda2a "github.com/IQVO/product-discovery/internal/adapters/inbound/a2a"
	"github.com/IQVO/product-discovery/internal/application/usecases"
)

// newAgent composes the A2A surface over the read side: the SAME read use
// cases the REST adapter uses (usecases.Queries), the Agent Card advertising
// agentURL, and the HTTP router around them. The agent never writes, so no
// inbox, unit of work, consumer or publisher is wired, and it calls no other
// context.
func newAgent(queries usecases.Queries, agentURL, version string) http.Handler {
	card := inbounda2a.Card(inbounda2a.Config{AgentURL: agentURL, Version: version})
	return newRouter(inbounda2a.Handler(inbounda2a.NewExecutor(inbounda2a.Deps{Queries: queries})), card)
}

// newRouter wraps the A2A handler in the process's HTTP surface:
//
//   - GET /healthz answers 200 {"status":"ok"}, so the Kubernetes probes
//     (charts/.../a2a-deployment.yaml) have a cheap target. A2A is
//     unauthenticated by decision, so probes need no credentials, and the route
//     leaks no skill, resource or state.
//   - The Agent Card is served at the well-known path
//     /.well-known/agent-card.json (the discovery contract, ADR 0007).
//   - The JSON-RPC endpoint is mounted at BOTH "/" (the canonical root, where
//     the gateway rewrites /api/a2a/product-discovery to) and "/a2a" (the
//     fleet's <CTX>_A2A_ENDPOINT convention).
func newRouter(a2aHandler http.Handler, card *a2a.AgentCard) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.Handle(a2asrv.WellKnownAgentCardPath, a2asrv.NewStaticAgentCardHandler(card))
	mux.Handle("/a2a", a2aHandler)
	mux.Handle("/", a2aHandler)
	return mux
}
