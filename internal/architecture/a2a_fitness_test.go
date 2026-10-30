package architecture

// A2A fitness functions (ADR 0007). The A2A inbound adapter is additive,
// never load-bearing, and READ-ONLY:
//
//   - TestA2AAdapterDependencyRule: the adapter depends only on the
//     application and domain layers, and nothing else in internal/ depends on
//     it (cmd/a2a, which wires it, lives outside internal/).
//   - TestA2ASourcesStayReadOnly: an ALLOWLIST (not a denylist of write names,
//     which a new write use case would slip past) of the identifiers the
//     adapter and cmd/a2a may use from the use-case, port and Postgres
//     packages. A write use case (usecases.Consumer, usecases.Rebuild...), a
//     transaction, the inbox, a Kafka adapter or an outbound A2A client makes
//     the test fail. The scan is AST-based, so a comment that merely mentions
//     a name does not trip it and an aliased import does not hide it.
//   - TestA2ASensorBites: the detector is proven against code fixtures, so the
//     sensor cannot pass vacuously.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	archgo "github.com/arch-go/arch-go/api"
	"github.com/arch-go/arch-go/api/configuration"
)

func TestA2AAdapterDependencyRule(t *testing.T) {
	moduleInfo := configuration.Load(modulePath)

	t.Run("a2a adapter depends only on application and domain", func(t *testing.T) {
		result := archgo.CheckArchitecture(moduleInfo, configuration.Config{
			DependenciesRules: []*configuration.DependenciesRule{
				{
					Package: "**.internal.adapters.inbound.a2a.**",
					ShouldOnlyDependsOn: &configuration.Dependencies{
						Internal: []string{
							"**.internal.adapters.inbound.a2a.**",
							"**.internal.application.**",
							"**.internal.domain.**",
						},
					},
				},
			},
		})

		assertArchGoPasses(t, result)
	})

	t.Run("nothing else depends on the a2a adapter", func(t *testing.T) {
		result := archgo.CheckArchitecture(moduleInfo, configuration.Config{
			DependenciesRules: []*configuration.DependenciesRule{
				{
					Package: "**.internal.**",
					ShouldNotDependsOn: &configuration.Dependencies{
						Internal: []string{"**.internal.adapters.inbound.a2a.**"},
					},
				},
			},
		})

		assertArchGoPasses(t, result)
	})
}

// a2aAllowed maps a package (by its last import-path element) to the exported
// identifiers the A2A sources may select from it. A package absent from the
// map is unrestricted; a listed one is an allowlist.
type a2aAllowed map[string][]string

// adapterAllowed is what internal/adapters/inbound/a2a may use: the read use
// case type and its answers, and the not-found sentinel.
var adapterAllowed = a2aAllowed{
	"usecases": {"Queries", "Product", "HitView", "SearchPage"},
	"ports":    {"ErrNotFound"},
}

// cmdAllowed is what cmd/a2a may use: the same read use cases, plus the Deps
// bundle they are built from, and ONLY the read-side Postgres adapters (no
// inbox, unit of work or listing map: the agent folds nothing).
var cmdAllowed = a2aAllowed{
	"usecases": {"Queries", "Deps"},
	"postgres": {"RunMigrations", "NewPool", "NewProductRepository", "NewCategoryRepository", "NewSearch"},
	"ports":    {},
}

// a2aForbiddenImports are import-path fragments no A2A source may import: an
// outbound A2A client (this context calls no sibling context), Kafka (no
// consumer, producer, outbox or relay: the agent consumes and publishes
// nothing) and the REST adapter (a second driving adapter, not a dependency).
var a2aForbiddenImports = map[string]string{
	"a2a-go/v2/a2aclient":       "an outbound A2A client (this context calls no sibling context; ADR 0007)",
	"kafka-go":                  "Kafka (the agent consumes and publishes nothing; ADR 0007)",
	"adapters/outbound/kafka":   "the Kafka adapter (the agent consumes and publishes nothing; ADR 0007)",
	"adapters/inbound/kafka":    "the Kafka consumers (they stay in cmd/api; ADR 0007)",
	"adapters/outbound/sources": "the rebuild sources (they stay in cmd/rebuild; ADR 0007)",
	"adapters/outbound/memory":  "the in-memory test doubles (real Postgres only)",
	"adapters/inbound/http":     "the REST adapter (a sibling driving adapter, not a dependency)",
}

// a2aViolations parses one Go source and reports every forbidden import and
// every selected identifier outside the allowlist.
func a2aViolations(filename string, src any, allowed a2aAllowed) ([]string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filename, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	var out []string
	names := map[string]string{} // local name -> package (last element)
	for _, imp := range f.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			return nil, err
		}
		out = append(out, forbiddenImport(path)...)
		names[importName(imp, path)] = lastElem(path)
	}
	ast.Inspect(f, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok {
			out = append(out, selectorViolation(fset, sel, names, allowed)...)
		}
		return true
	})
	return out, nil
}

func forbiddenImport(path string) []string {
	var out []string
	for frag, why := range a2aForbiddenImports {
		if strings.Contains(path, frag) {
			out = append(out, "imports "+path+": "+why)
		}
	}
	sort.Strings(out)
	return out
}

func importName(imp *ast.ImportSpec, path string) string {
	if imp.Name != nil {
		return imp.Name.Name
	}
	return lastElem(path)
}

func lastElem(path string) string {
	return path[strings.LastIndex(path, "/")+1:]
}

func selectorViolation(fset *token.FileSet, sel *ast.SelectorExpr, names map[string]string, allowed a2aAllowed) []string {
	x, ok := sel.X.(*ast.Ident)
	if !ok {
		return nil
	}
	pkg, ok := names[x.Name]
	if !ok {
		return nil
	}
	list, restricted := allowed[pkg]
	if !restricted || slices.Contains(list, sel.Sel.Name) {
		return nil
	}
	return []string{fset.Position(sel.Pos()).String() + ": uses " + pkg + "." + sel.Sel.Name + ", which is not on the read-only allowlist"}
}

func TestA2ASourcesStayReadOnly(t *testing.T) {
	scans := []struct {
		root    string
		allowed a2aAllowed
	}{
		{"../adapters/inbound/a2a", adapterAllowed},
		{"../../cmd/a2a", cmdAllowed},
	}
	for _, scan := range scans {
		files := goFilesUnder(t, scan.root, false)
		if len(files) == 0 {
			t.Fatalf("no A2A sources under %s; the sensor is looking in the wrong place", scan.root)
		}
		for _, path := range files {
			src, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			found, err := a2aViolations(path, src, scan.allowed)
			if err != nil {
				t.Fatalf("parse %s: %v", path, err)
			}
			for _, v := range found {
				t.Errorf("%s\n  WHY: the A2A agent is read-only (ADR 0007): two read skills over usecases.Queries, no write use case, no transaction, no Kafka, no outbound A2A client. Do NOT widen the allowlist; a write skill needs its own ADR first.", v)
			}
		}
	}
}

// TestA2ASensorBites proves the detector flags each forbidden shape and passes
// the legitimate ones, so TestA2ASourcesStayReadOnly cannot pass vacuously.
func TestA2ASensorBites(t *testing.T) {
	const head = "package x\n\nimport (\n%s\n)\n\nfunc f() { %s }\n"
	cases := []struct {
		name, imports, body string
		allowed             a2aAllowed
		want                string // "" = clean
	}{
		{"write use case", `"github.com/IQVO/product-discovery/internal/application/usecases"`, "_ = usecases.Consumer{}", adapterAllowed, "usecases.Consumer"},
		{"rebuild use case", `"github.com/IQVO/product-discovery/internal/application/usecases"`, "_ = usecases.Rebuild{}", adapterAllowed, "usecases.Rebuild"},
		{"aliased write use case", `uc "github.com/IQVO/product-discovery/internal/application/usecases"`, "_ = uc.Consumer{}", adapterAllowed, "usecases.Consumer"},
		{"unit of work port", `"github.com/IQVO/product-discovery/internal/application/ports"`, "var _ ports.UnitOfWork", adapterAllowed, "ports.UnitOfWork"},
		{"inbox adapter in cmd", `"github.com/IQVO/product-discovery/internal/adapters/outbound/postgres"`, "_ = postgres.NewInbox(nil)", cmdAllowed, "postgres.NewInbox"},
		{"unit of work adapter in cmd", `"github.com/IQVO/product-discovery/internal/adapters/outbound/postgres"`, "_ = postgres.NewUnitOfWork(nil)", cmdAllowed, "postgres.NewUnitOfWork"},
		{"outbound a2a client", `"github.com/a2aproject/a2a-go/v2/a2aclient"`, "", adapterAllowed, "a2aclient"},
		{"kafka adapter", `"github.com/IQVO/product-discovery/internal/adapters/outbound/kafka"`, "", cmdAllowed, "outbound/kafka"},
		{"kafka driver", `"github.com/segmentio/kafka-go"`, "", cmdAllowed, "kafka-go"},
		{"consumers", `"github.com/IQVO/product-discovery/internal/adapters/inbound/kafka"`, "", cmdAllowed, "inbound/kafka"},
		{"memory double", `"github.com/IQVO/product-discovery/internal/adapters/outbound/memory"`, "", cmdAllowed, "outbound/memory"},
		{"rest adapter", `inboundhttp "github.com/IQVO/product-discovery/internal/adapters/inbound/http"`, "", adapterAllowed, "inbound/http"},
		{"read use case", `"github.com/IQVO/product-discovery/internal/application/usecases"`, "_ = usecases.Queries{}", adapterAllowed, ""},
		{"read-side postgres", `"github.com/IQVO/product-discovery/internal/adapters/outbound/postgres"`, "_ = postgres.NewSearch(nil)", cmdAllowed, ""},
		{"not-found sentinel", `"github.com/IQVO/product-discovery/internal/application/ports"`, "_ = ports.ErrNotFound", adapterAllowed, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			found, err := a2aViolations("fixture.go", fmt.Sprintf(head, tc.imports, tc.body), tc.allowed)
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.Join(found, "\n"); tc.want == "" && got != "" || tc.want != "" && !strings.Contains(got, tc.want) {
				t.Fatalf("violations = %q, want %q", got, tc.want)
			}
		})
	}

	// A comment that merely mentions a write use case is not a use of it.
	found, err := a2aViolations("fixture.go", "package x\n\n// usecases.Consumer is never used here, nor postgres.NewInbox.\nfunc f() {}\n", adapterAllowed)
	if err != nil || len(found) != 0 {
		t.Fatalf("a comment must not trip the sensor: %v %v", found, err)
	}
}
