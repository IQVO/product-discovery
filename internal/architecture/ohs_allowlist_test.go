package architecture

// TestNoInOrgNetworkRoute (storefront campaign sensor): the outbound HTTP
// clients of this service may only call routes listed in
// docs/network-ohs-allowlist.yaml, and no route may contain a forbidden
// fragment. Route paths are the string literals of internal/adapters/outbound/
// sources that start with "/"; the HTTP client code of the service lives only
// in that package (no other outbound adapter may import net/http).
//
// Limits: it reads literals, so a path assembled at run time from pieces would
// escape it; the sources package builds every path from named constants.

import (
	"go/ast"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

var (
	allowedPathRx = regexp.MustCompile(`(?m)path:\s*(/[^\s,}]+)`)
	forbiddenRx   = regexp.MustCompile(`(?m)^\s+-\s+"?([^"\s]+)"?\s*$`)
	routeLiteral  = regexp.MustCompile(`^/[a-z][A-Za-z0-9/_{}:.-]*$`)
)

// allowlist parses the routes and the forbidden fragments of the allowlist file.
func allowlist(t *testing.T) (routes map[string]bool, forbidden []string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "docs", "network-ohs-allowlist.yaml"))
	if err != nil {
		t.Fatalf("read the allowlist: %v", err)
	}
	return parseAllowlist(string(raw))
}

func parseAllowlist(raw string) (map[string]bool, []string) {
	routes := map[string]bool{}
	for _, m := range allowedPathRx.FindAllStringSubmatch(raw, -1) {
		routes[m[1]] = true
	}
	var forbidden []string
	if i := strings.Index(raw, "\nforbidden:"); i >= 0 {
		for _, m := range forbiddenRx.FindAllStringSubmatch(raw[i:], -1) {
			forbidden = append(forbidden, m[1])
		}
	}
	return routes, forbidden
}

// routeViolations returns what is wrong with the route literals of one file.
func routeViolations(literals []string, routes map[string]bool, forbidden []string) []string {
	var bad []string
	for _, lit := range literals {
		if !routeLiteral.MatchString(lit) {
			continue
		}
		if !routes[lit] {
			bad = append(bad, lit+" is not in docs/network-ohs-allowlist.yaml")
			continue
		}
		for _, f := range forbidden {
			if strings.Contains(lit, f) {
				bad = append(bad, lit+" contains the forbidden fragment "+f)
			}
		}
	}
	return bad
}

func stringLiterals(t *testing.T, path string) []string {
	t.Helper()
	_, f := parseFile(t, path)
	var out []string
	ast.Inspect(f, func(n ast.Node) bool {
		if bl, ok := n.(*ast.BasicLit); ok && bl.Kind.String() == "STRING" {
			if s, err := strconv.Unquote(bl.Value); err == nil {
				out = append(out, s)
			}
		}
		return true
	})
	return out
}

func TestNoInOrgNetworkRoute(t *testing.T) {
	routes, forbidden := allowlist(t)
	if len(routes) == 0 || len(forbidden) == 0 {
		t.Fatalf("the allowlist must declare routes and forbidden fragments, got %v %v", routes, forbidden)
	}
	var problems []string
	for _, p := range goFiles(t, filepath.Join("internal", "adapters", "outbound", "sources")) {
		for _, v := range routeViolations(stringLiterals(t, p), routes, forbidden) {
			problems = append(problems, p+": "+v)
		}
	}
	for _, p := range goFiles(t, filepath.Join("internal", "adapters", "outbound")) {
		if !strings.Contains(filepath.ToSlash(p), "/outbound/sources/") {
			_, f := parseFile(t, p)
			if importsAny(f, "net/http") {
				problems = append(problems, p+": outbound HTTP clients belong in internal/adapters/outbound/sources")
			}
		}
	}
	sort.Strings(problems)
	if len(problems) > 0 {
		t.Errorf("%s", archViolation("contents", "outbound routes are on the allowlist", strings.Join(problems, "; ")+
			". FIX: call only routes in docs/network-ohs-allowlist.yaml (model inbound rows for shopper / storefront)"))
	}
}

func TestNoInOrgNetworkRouteSensorFailsOnBadFixtures(t *testing.T) {
	routes, forbidden := parseAllowlist("routes:\n  - {path: /catalog}\n  - {path: /promises:confirm}\n\nforbidden:\n  - \":confirm\"\n  - ship-to\n")
	if len(routes) != 2 || len(forbidden) != 2 {
		t.Fatalf("parse: %v %v", routes, forbidden)
	}
	if got := routeViolations([]string{"/catalog", "text", "/Not-A-Route"}, routes, forbidden); len(got) != 0 {
		t.Errorf("a listed route and non-routes must pass, got %v", got)
	}
	if got := routeViolations([]string{"/orders/{id}/ship-to"}, routes, forbidden); len(got) != 1 {
		t.Errorf("an unlisted route must fail, got %v", got)
	}
	if got := routeViolations([]string{"/promises:confirm"}, routes, forbidden); len(got) != 1 || !strings.Contains(got[0], "forbidden") {
		t.Errorf("a listed route with a forbidden fragment must fail, got %v", got)
	}
}
