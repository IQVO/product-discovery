// Consumer contract tests shared by the three pinned producers (ADR pattern:
// consumer-contract-coverage-wave). The three testdata/*.openapi.yaml files are
// byte copies registered in contracts/pinned.json; the pinned-contracts CI job
// fails when a copy drifts from the producer's develop. R1..R4 per producer live
// in the per-producer files; this file carries the helpers and the generic
// client's problem+json -> sentinel behaviour, which is producer-independent.
package sources

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
)

const (
	specOfferAvailability = "offer-availability.openapi.yaml"
	specPricing           = "pricing.openapi.yaml"
	specProductContent    = "product-content.openapi.yaml"
)

// loadPinnedSpec loads one pinned producer spec. NEVER edit the copy: refresh
// means copying the producer's apis/openapi.yaml over it (contracts/pinned.json).
func loadPinnedSpec(t *testing.T, file string) *openapi3.T {
	t.Helper()
	doc, err := openapi3.NewLoader().LoadFromFile("testdata/" + file)
	if err != nil {
		t.Fatalf("load pinned spec %s: %v", file, err)
	}
	return doc
}

// pinnedSchema returns a named component schema of a pinned spec.
func pinnedSchema(t *testing.T, doc *openapi3.T, name string) *openapi3.Schema {
	t.Helper()
	ref, ok := doc.Components.Schemas[name]
	if !ok || ref.Value == nil {
		t.Fatalf("pinned %s spec has no %s schema", doc.Info.Title, name)
	}
	return ref.Value
}

// operationSchema returns the response body schema the pinned spec declares for
// one (path, status, media type); it fails the test when the pin lacks any of
// them, because the gateway decodes exactly that schema on a 200.
func operationSchema(t *testing.T, doc *openapi3.T, path string, status int, media string) *openapi3.Schema {
	t.Helper()
	item := doc.Paths.Find(path)
	if item == nil || item.Get == nil {
		t.Fatalf("pinned %s spec has no GET %s", doc.Info.Title, path)
	}
	resp := item.Get.Responses.Status(status)
	if resp == nil || resp.Value == nil {
		t.Fatalf("pinned %s spec: GET %s declares no %d response", doc.Info.Title, path, status)
	}
	content, ok := resp.Value.Content[media]
	if !ok || content.Schema == nil || content.Schema.Value == nil {
		t.Fatalf("pinned %s spec: GET %s %d declares no %s schema", doc.Info.Title, path, status, media)
	}
	return content.Schema.Value
}

// fixture reads one testdata example; every fixture is DERIVED from the pinned
// schemas (no producer ships response examples yet) — see testdata/README.md.
func fixture(t *testing.T, file string) []byte {
	t.Helper()
	body, err := os.ReadFile("testdata/" + file)
	if err != nil {
		t.Fatalf("read fixture %s: %v", file, err)
	}
	return body
}

// assertSatisfiesSchema is R2: a fixture must validate against the pinned schema.
func assertSatisfiesSchema(t *testing.T, what string, s *openapi3.Schema, body []byte) {
	t.Helper()
	var data any
	if err := json.Unmarshal(body, &data); err != nil {
		t.Fatalf("%s: fixture is not JSON: %v", what, err)
	}
	if err := s.VisitJSON(data); err != nil {
		t.Errorf("%s does not satisfy the pinned schema: %v", what, err)
	}
}

// callSite is one (method, path) the generic client can request on a producer.
// Paged marks the routes where the client sends limit/cursor query parameters.
type callSite struct {
	method string
	path   string
	opID   string
	paged  bool
}

// assertSpecCoversCallSites is R1: every route the code calls (the path
// constants, not retyped strings) must exist in the pin, keep its operationId
// (a producer rename must fail here, not in production), and — where the client
// pages — declare the limit and cursor query parameters it sends.
func assertSpecCoversCallSites(t *testing.T, doc *openapi3.T, sites []callSite) {
	t.Helper()
	for _, cs := range sites {
		item := doc.Paths.Find(cs.path)
		if item == nil {
			t.Errorf("pinned %s spec has no %s %s that the client calls", doc.Info.Title, cs.method, cs.path)
			continue
		}
		op := item.Get
		if cs.method != http.MethodGet || op == nil {
			t.Errorf("pinned %s spec: %s %s is not a GET", doc.Info.Title, cs.method, cs.path)
			continue
		}
		if op.OperationID != cs.opID {
			t.Errorf("%s %s is operationId %q, expected %q", cs.method, cs.path, op.OperationID, cs.opID)
		}
		if cs.paged {
			assertDeclaresQueryParam(t, doc, cs.path, item.Parameters, op.Parameters, "limit")
			assertDeclaresQueryParam(t, doc, cs.path, item.Parameters, op.Parameters, "cursor")
		}
	}
}

func assertDeclaresQueryParam(t *testing.T, doc *openapi3.T, path string, pathLevel, opLevel openapi3.Parameters, name string) {
	t.Helper()
	for _, group := range []openapi3.Parameters{pathLevel, opLevel} {
		for _, p := range group {
			if p.Value != nil && p.Value.Name == name && p.Value.In == "query" {
				return
			}
		}
	}
	t.Errorf("pinned %s spec: GET %s does not declare the %s query parameter the client sends", doc.Info.Title, path, name)
}

// effectiveProperties merges a schema's own properties with those of its allOf
// branches (ProductContent is an allOf of an identity object and ContentBody),
// so R4 can ask "is this json tag declared anywhere in the schema".
func effectiveProperties(s *openapi3.Schema) map[string]*openapi3.Schema {
	props := make(map[string]*openapi3.Schema, len(s.Properties))
	for k, ref := range s.Properties {
		if ref.Value != nil {
			props[k] = ref.Value
		}
	}
	for _, sub := range s.AllOf {
		if sub.Value == nil {
			continue
		}
		for k, v := range effectiveProperties(sub.Value) {
			props[k] = v
		}
	}
	return props
}

// effectiveRequired is effectiveProperties' twin for the required list.
func effectiveRequired(s *openapi3.Schema) map[string]bool {
	req := make(map[string]bool, len(s.Required))
	for _, k := range s.Required {
		req[k] = true
	}
	for _, sub := range s.AllOf {
		if sub.Value != nil {
			for k := range effectiveRequired(sub.Value) {
				req[k] = true
			}
		}
	}
	return req
}

// fieldsSpecified is R4's first half: every json-tagged field the DTO decodes
// must be declared by the pinned schema with a compatible type. It returns the
// optional ones (declared but not required) for the absence-tolerance half.
func fieldsSpecified(t *testing.T, what string, s *openapi3.Schema, ty reflect.Type) map[string]bool {
	t.Helper()
	props, required := effectiveProperties(s), effectiveRequired(s)
	optional := make(map[string]bool)
	for i := range ty.NumField() {
		f := ty.Field(i)
		key := strings.Split(f.Tag.Get("json"), ",")[0]
		if key == "" || key == "-" {
			t.Fatalf("%s: field %s needs a json tag to be checked against the pin", what, f.Name)
		}
		prop, ok := props[key]
		if !ok {
			t.Errorf("%s reads %q, which the pinned schema does not declare", what, key)
			continue
		}
		if !typeCompatible(f.Type, prop) {
			t.Errorf("%s.%s: pinned type %q is not compatible with Go %s", what, key, schemaTypeName(prop), f.Type)
		}
		if !required[key] {
			optional[key] = true
		}
	}
	return optional
}

func typeCompatible(ft reflect.Type, s *openapi3.Schema) bool {
	// A pure $ref/allOf composition carries no type of its own.
	if len(s.AllOf) > 0 || len(s.OneOf) > 0 || len(s.AnyOf) > 0 {
		return true
	}
	if ft == reflect.TypeOf(time.Time{}) {
		return schemaTypeName(s) == "string"
	}
	switch ft.Kind() {
	case reflect.String:
		return schemaTypeName(s) == "string"
	case reflect.Bool:
		return schemaTypeName(s) == "boolean"
	case reflect.Int64, reflect.Int:
		return schemaTypeName(s) == "integer"
	case reflect.Slice:
		return schemaTypeName(s) == "array"
	case reflect.Struct:
		return schemaTypeName(s) == "object"
	default:
		return false
	}
}

func schemaTypeName(s *openapi3.Schema) string {
	if s.Type == nil || len(*s.Type) == 0 {
		return ""
	}
	return (*s.Type)[0]
}

// assertDecodes re-checks the page shape against the pin inside an R3 run: the
// decoded envelope must stay exactly {items, nextCursor}.
func assertDecodes(t *testing.T, doc *openapi3.T, path string) {
	t.Helper()
	s := operationSchema(t, doc, path, 200, "application/json")
	for key := range s.Properties {
		if key != "items" && key != "nextCursor" {
			t.Errorf("pinned %s %s response gained key %q the page envelope does not know", doc.Info.Title, path, key)
		}
	}
}

// sampleFor builds a minimal schema-legal value (every required key, no
// optional one); the values need only satisfy encoding/json, never the enums.
func sampleFor(s *openapi3.Schema) any {
	if len(s.AllOf) > 0 {
		return sampleAllOf(s)
	}
	switch schemaTypeName(s) {
	case "string":
		if s.Format == "date-time" {
			return "2026-01-02T15:04:05Z"
		}
		return "x"
	case "integer":
		return 1
	case "boolean":
		return false
	case "array":
		return []any{}
	default: // object, $ref or schemaless: the required keys only
		out := map[string]any{}
		for k, sub := range effectiveProperties(s) {
			if effectiveRequired(s)[k] {
				out[k] = sampleFor(sub)
			}
		}
		return out
	}
}

// sampleAllOf merges object branches (ProductContent = identity object +
// ContentBody); a lone scalar branch governs the whole value (mediaId: allOf
// [$ref Id]).
func sampleAllOf(s *openapi3.Schema) any {
	merged := map[string]any{}
	var scalar any
	for _, sub := range s.AllOf {
		if sub.Value == nil {
			continue
		}
		if m, ok := sampleFor(sub.Value).(map[string]any); ok {
			for k, v := range m {
				merged[k] = v
			}
		} else {
			scalar = sampleFor(sub.Value)
		}
	}
	if len(merged) > 0 {
		return merged
	}
	return scalar
}

// toleratesAbsence is R4's second half: a body that carries only the schema's
// REQUIRED keys must decode without error, because the producer may omit every
// optional field and these DTOs keep a documented zero value for each.
func toleratesAbsence(t *testing.T, what string, s *openapi3.Schema, dto any) {
	t.Helper()
	body, err := json.Marshal(sampleFor(s))
	if err != nil {
		t.Fatalf("%s: build required-only body: %v", what, err)
	}
	if err := json.Unmarshal(body, dto); err != nil {
		t.Errorf("%s: a body with only required keys (%s) must decode, got %v", what, body, err)
	}
}

// rowType and elemType keep the R4 call sites reading as data, not reflection.
func rowType[T any](v T) reflect.Type { return reflect.TypeOf(v) }

func elemType(t *testing.T, v any) reflect.Type {
	t.Helper()
	ty := reflect.TypeOf(v)
	if ty.Kind() != reflect.Slice {
		t.Fatalf("elemType needs a slice, got %s", ty)
	}
	return ty.Elem()
}

// upstream records what the client actually sent for one serveBody server.
type upstream struct {
	hits  atomic.Int32
	path  string
	query url.Values
}

// serveBody stands up one upstream answering every request identically; tests
// shrink Pause so a retried 5xx does not slow the run.
func serveBody(t *testing.T, status int, ctype, body string, up *upstream) Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		up.hits.Add(1)
		up.path, up.query = r.URL.Path, r.URL.Query()
		w.Header().Set("Content-Type", ctype)
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return Client{BaseURL: srv.URL, HTTP: srv.Client(), Pause: time.Millisecond}
}

// TestProblemJSONSurfacesAsSentinelWithoutRetry: every pinned spec answers a
// bad list read with RFC 9457 problem+json (offer-availability BadRequest,
// pricing InvalidQuery, product-content BadRequestPage). The generic client
// must surface that as ErrUnexpectedStatus on the first answer, never retry a
// 4xx, and never try to decode the problem body as a page.
func TestProblemJSONSurfacesAsSentinelWithoutRetry(t *testing.T) {
	const problem = `{"type":"/problems/invalid-cursor","title":"unknown cursor","status":400}`
	up := &upstream{}
	cl := serveBody(t, http.StatusBadRequest, "application/problem+json", problem, up)
	oa := OfferAvailability{Client: cl}
	if _, err := oa.ListCatalog(t.Context(), "bogus", 1); !errors.Is(err, ErrUnexpectedStatus) {
		t.Fatalf("a 400 problem+json must surface as ErrUnexpectedStatus, got %v", err)
	}
	if up.hits.Load() != 1 {
		t.Errorf("a 4xx must not be retried: %d requests", up.hits.Load())
	}
}

// Test5xxIsRetriedThenFails: the specs declare 500 InternalError answers; the
// client retries those Attempts times and then fails with the same sentinel.
func Test5xxIsRetriedThenFails(t *testing.T) {
	up := &upstream{}
	cl := serveBody(t, http.StatusInternalServerError, "application/problem+json", `{}`, up)
	pc := ProductContent{Client: cl}
	if _, err := pc.ListCategories(t.Context()); !errors.Is(err, ErrUnexpectedStatus) {
		t.Fatalf("a 500 must end as ErrUnexpectedStatus, got %v", err)
	}
	if up.hits.Load() != Attempts {
		t.Errorf("a 5xx must be tried exactly Attempts=%d times, got %d", Attempts, up.hits.Load())
	}
}
