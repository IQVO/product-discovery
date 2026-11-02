package a2a

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"github.com/IQVO/product-discovery/internal/application/ports"
	"github.com/IQVO/product-discovery/internal/application/usecases"
	"github.com/IQVO/product-discovery/internal/domain/search"
)

// tracerName is the OTel instrumentation scope for A2A skill spans.
const tracerName = "github.com/IQVO/product-discovery/internal/adapters/inbound/a2a"

// Skill ids advertised on the Agent Card (see Card). They follow the REST
// operations they are backed by (searchProducts, getProduct).
const (
	skillSearchProducts = "search_products"
	skillGetProduct     = "get_product"
)

// maxIDLength bounds a catalogue item identifier; a longer one cannot name an
// item (the REST adapter's maxIDLength).
const maxIDLength = 128

// skillRequest is the wire contract of every skill call: the first message
// part carries (as a Data part, or a Text part holding the same JSON) the
// skill id and its arguments. Strict on purpose: an agent client is
// machine-to-machine, so unknown fields are rejected, never ignored, and a
// misspelled name like "inStok" must not silently drop the filter. (Go's
// encoding/json matches field names case-insensitively, so "instock" still
// reaches inStock; only genuinely unknown names are refused.)
type skillRequest struct {
	Skill     string          `json:"skill"`
	Arguments json.RawMessage `json:"arguments"`
}

// skillResult is what a skill hands back: the structured data and the one-line
// summary for the reply's text part.
type skillResult struct {
	data    skillData
	summary string
}

// skillFunc runs one skill over the read use cases.
type skillFunc func(ctx context.Context, q usecases.Queries, rawArgs json.RawMessage) (skillResult, *problem)

// skills is the dispatch table; its keys are exactly the card's skill ids
// (TestCardSkillsMatchTheDispatchTable).
var skills = map[string]skillFunc{
	skillSearchProducts: searchProducts,
	skillGetProduct:     getProduct,
}

// answer maps one incoming message to a skill and returns the agent's reply
// Message. It is the whole executor body: this is a message-only agent, so a
// skill run is exactly one request mapped to one answer (ADR 0007).
func answer(ctx context.Context, deps Deps, ec *a2asrv.ExecutorContext) a2a.Event {
	req, p := parseSkillRequest(ec)
	if p != nil {
		return reply(ec, envelope{Problem: p}, p.summary())
	}
	run, ok := skills[req.Skill]
	if !ok {
		// No span: the name is client-chosen text and must not become a span
		// name (unbounded cardinality).
		p := problemUnknownSkill(req.Skill)
		return reply(ec, envelope{Skill: req.Skill, Problem: p}, p.summary())
	}

	ctx, span := otel.Tracer(tracerName).Start(ctx, "a2a.skill "+req.Skill)
	defer span.End()
	span.SetAttributes(attribute.String("a2a.skill", req.Skill))

	res, p := run(ctx, deps.Queries, req.Arguments)
	if p != nil {
		span.SetStatus(codes.Error, p.Detail)
		return reply(ec, envelope{Skill: req.Skill, Problem: p}, p.summary())
	}
	return reply(ec, envelope{Skill: req.Skill, Data: &res.data}, res.summary)
}

// reply builds the agent Message for one skill run: a Data part carrying the
// structured envelope and a Text part carrying the one-line summary, bound to
// the caller's context (and task, when the client referenced one).
func reply(ec *a2asrv.ExecutorContext, env envelope, summary string) a2a.Event {
	return a2a.NewMessageForTask(a2a.MessageRoleAgent, ec, a2a.NewDataPart(env), a2a.NewTextPart(summary))
}

// parseSkillRequest extracts the skillRequest from the message's first part. A
// Data part (or a Text part holding the same JSON object) is accepted;
// anything else is a request-shape problem, answered, not raised, because a
// malformed delegation is a business answer for the calling agent, not a
// transport failure.
func parseSkillRequest(ec *a2asrv.ExecutorContext) (skillRequest, *problem) {
	msg := ec.Message
	if msg == nil || len(msg.Parts) == 0 {
		return skillRequest{}, problemMalformed("the message carries no parts")
	}
	raw, err := partJSON(msg.Parts[0])
	if err != nil {
		return skillRequest{}, problemMalformed(fmt.Sprintf("the first message part is not a skill request: %v", err))
	}
	var req skillRequest
	if err := decodeStrict(raw, &req); err != nil {
		var typeErr *json.UnmarshalTypeError
		if errors.As(err, &typeErr) {
			// Keep Go type names out of the answer.
			return skillRequest{}, problemMalformed(`the skill request must be a JSON object whose "skill" is a string`)
		}
		return skillRequest{}, problemMalformed(fmt.Sprintf("the skill request is not valid: %v", err))
	}
	if req.Skill == "" {
		return skillRequest{}, problemMalformed(`the skill request carries no "skill" field`)
	}
	return req, nil
}

// partJSON renders one part's content as the JSON bytes of a skill request.
func partJSON(part *a2a.Part) ([]byte, error) {
	switch content := part.Content.(type) {
	case a2a.Data:
		return json.Marshal(content.Value)
	case a2a.Text:
		return []byte(string(content)), nil
	default:
		return nil, errors.New("expected a data part (or a text part holding the skill JSON)")
	}
}

// decodeStrict decodes one JSON document into dst, rejecting unknown fields
// and trailing data. Absent arguments decode as the empty object so each
// skill's own required-field checks produce the specific problem.
func decodeStrict(raw []byte, dst any) error {
	if len(bytes.TrimSpace(raw)) == 0 {
		raw = []byte("{}")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	if dec.More() {
		return errors.New("unexpected data after the JSON document")
	}
	return nil
}

// searchProductsArgs are the arguments of search_products: the query
// parameters of GET /products under the same names.
type searchProductsArgs struct {
	Q          string `json:"q"`
	CategoryID string `json:"categoryId"`
	InStock    *bool  `json:"inStock"`
	PriceMin   *int64 `json:"priceMin"`
	PriceMax   *int64 `json:"priceMax"`
	Sort       string `json:"sort"`
	After      string `json:"after"`
	Limit      *int   `json:"limit"`
}

// getProductArgs are the arguments of get_product.
type getProductArgs struct {
	CatalogItemID string `json:"catalogItemId"`
}

// searchProducts is the discovery skill, through the same SearchProducts use
// case GET /products uses.
func searchProducts(ctx context.Context, q usecases.Queries, rawArgs json.RawMessage) (skillResult, *problem) {
	var args searchProductsArgs
	if p := decodeArgs(rawArgs, &args, true); p != nil {
		return skillResult{}, p
	}
	if containsNUL(args.Q, args.CategoryID, args.Sort, args.After) {
		// The REST adapter's 400 for a NUL in the query string: PostgreSQL text
		// cannot hold U+0000 and a statement carrying one aborts the transaction.
		return skillResult{}, problemMalformed("a NUL character (U+0000) is not allowed in the arguments")
	}
	in := search.Input{
		Text: args.Q, CategoryID: args.CategoryID, InStock: args.InStock,
		PriceMin: args.PriceMin, PriceMax: args.PriceMax, Sort: args.Sort, After: args.After,
	}
	if args.Limit != nil {
		if *args.Limit == 0 {
			// REST: limit=0 is refused, not defaulted.
			return skillResult{}, problemFromUseCase(&search.ValidationError{Field: "limit", Reason: limitReason}, "")
		}
		in.Limit = *args.Limit
	}
	page, err := q.SearchProducts(ctx, in)
	if err != nil {
		return skillResult{}, problemFromUseCase(err, "")
	}
	dto := searchDTO(page)
	return skillResult{data: skillData{Search: &dto}, summary: summarizeSearch(dto)}, nil
}

// getProduct is the single-product read skill, through the same GetProduct use
// case GET /products/{catalogItemId} uses.
func getProduct(ctx context.Context, q usecases.Queries, rawArgs json.RawMessage) (skillResult, *problem) {
	var args getProductArgs
	if p := decodeArgs(rawArgs, &args, false); p != nil {
		return skillResult{}, p
	}
	id := args.CatalogItemID
	if id == "" {
		return skillResult{}, problemMalformed("catalogItemId is required")
	}
	if len(id) > maxIDLength || containsNUL(id) {
		// The REST adapter answers 404 for an identifier that cannot name an
		// item (over-long, or carrying a NUL).
		return skillResult{}, problemFromUseCase(ports.ErrNotFound, id)
	}
	product, err := q.GetProduct(ctx, id)
	if err != nil {
		return skillResult{}, problemFromUseCase(err, id)
	}
	dto := productDTO(product)
	return skillResult{data: skillData{Product: &dto}, summary: summarizeProduct(dto)}, nil
}

// limitReason is the REST adapter's validation text for a bad limit.
const limitReason = "must be an integer between 1 and 500"

// decodeArgs decodes a skill's arguments strictly. For the search arguments
// (asQuery), a value of the wrong JSON type is the same 422 validation-failed
// the REST adapter answers for a query parameter of the wrong type; anywhere
// else, and for an unknown name, a syntax error or trailing data, it is a 400
// malformed request.
func decodeArgs(raw json.RawMessage, dst any, asQuery bool) *problem {
	err := decodeStrict(raw, dst)
	if err == nil {
		return nil
	}
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) {
		if typeErr.Field == "" {
			return problemMalformed("arguments must be a JSON object")
		}
		if !asQuery {
			return problemMalformed(fmt.Sprintf("arguments: %s must be a %s", typeErr.Field, typeErr.Type.Kind()))
		}
		return problemFromUseCase(&search.ValidationError{Field: typeErr.Field, Reason: typeReason(typeErr.Field, typeErr.Type.Kind().String())}, "")
	}
	return problemMalformed(fmt.Sprintf("arguments: %v", err))
}

// typeReason is the REST adapter's validation text for a parameter of the
// wrong type.
func typeReason(field, kind string) string {
	switch {
	case field == "limit":
		return limitReason
	case strings.HasPrefix(kind, "bool"):
		return "must be true or false"
	case strings.HasPrefix(kind, "int"):
		return "must be an integer"
	default:
		return "must be a string"
	}
}

func containsNUL(values ...string) bool {
	for _, v := range values {
		if strings.ContainsRune(v, 0) {
			return true
		}
	}
	return false
}

// problemFromUseCase maps a use case error to a problem answer, using the SAME
// classification as the REST adapter (internal/adapters/inbound/http
// problemCatalogue) for the errors these reads can raise: ports.ErrNotFound is
// 404, search.ErrValidation is 422 and anything unrecognised is 500, whose
// detail never leaks the internal message (it is logged instead). The not-found
// detail is adapter-owned text.
func problemFromUseCase(err error, itemID string) *problem {
	switch {
	case errors.Is(err, ports.ErrNotFound):
		return problemNotFound(fmt.Sprintf("no product exists with catalogItemId %q", truncate(itemID)))
	case errors.Is(err, search.ErrValidation):
		return problemValidation(err.Error())
	default:
		slog.Error("a2a skill failed", "error", err)
		return problemInternal()
	}
}

// truncate bounds an echoed identifier so an over-long one is not reflected
// back in full.
func truncate(s string) string {
	const limit = 64
	if r := []rune(s); len(r) > limit {
		return string(r[:limit]) + "..."
	}
	return s
}
