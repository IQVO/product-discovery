package architecture

// MCP read-only fitness function (ADR 0009). The MCP inbound adapter is
// additive and READ-ONLY: TestMCPSourcesStayReadOnly applies the same
// ALLOWLIST detector as TestA2ASourcesStayReadOnly (a2aViolations: AST-based,
// aliased imports do not hide a use, a comment that merely mentions a name does
// not trip it) to internal/adapters/inbound/mcp and cmd/mcp. A write use case
// (usecases.Consumer, usecases.Rebuilder...), a transaction, the inbox, a Kafka
// adapter, the in-memory doubles or the REST adapter makes the test fail.
// TestMCPAdapterDependencyRule (fitness_test.go) keeps the import graph honest
// in both directions: nothing but cmd/mcp imports the adapter.

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// mcpAdapterAllowed is what internal/adapters/inbound/mcp may use: the read
// use case answers and the not-found sentinel.
var mcpAdapterAllowed = a2aAllowed{
	"usecases": {"Product", "HitView", "SearchPage"},
	"ports":    {"ErrNotFound"},
}

// mcpCmdAllowed is what cmd/mcp may use: the same read side as cmd/a2a.
var mcpCmdAllowed = cmdAllowed

func TestMCPSourcesStayReadOnly(t *testing.T) {
	scans := []struct {
		root    string
		allowed a2aAllowed
	}{
		{"../adapters/inbound/mcp", mcpAdapterAllowed},
		{"../../cmd/mcp", mcpCmdAllowed},
	}
	for _, scan := range scans {
		if _, err := os.Stat(scan.root); os.IsNotExist(err) {
			t.Skipf("no %s in this service: nothing to check", scan.root)
		}
		files := goFilesUnder(t, scan.root, false)
		if len(files) == 0 {
			t.Fatalf("no MCP sources under %s; the sensor is looking in the wrong place", scan.root)
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
				t.Errorf("%s\n  WHY: the MCP server is read-only (ADR 0009): two read tools over the read use cases, no write use case, no transaction, no Kafka, no REST adapter. Do NOT widen the allowlist; a write tool needs its own ADR first.", v)
			}
		}
	}
}

// TestMCPSensorBites proves the detector flags each forbidden shape under the
// MCP allowlists and passes the legitimate ones, so TestMCPSourcesStayReadOnly
// cannot pass vacuously.
func TestMCPSensorBites(t *testing.T) {
	const head = "package x\n\nimport (\n%s\n)\n\nfunc f() { %s }\n"
	cases := []struct {
		name, imports, body string
		allowed             a2aAllowed
		want                string // "" = clean
	}{
		{"write use case", `"github.com/IQVO/product-discovery/internal/application/usecases"`, "_ = usecases.Consumer{}", mcpAdapterAllowed, "usecases.Consumer"},
		{"rebuild use case", `"github.com/IQVO/product-discovery/internal/application/usecases"`, "_ = usecases.Rebuilder{}", mcpAdapterAllowed, "usecases.Rebuilder"},
		{"the whole Queries bundle is not the adapter's seam", `"github.com/IQVO/product-discovery/internal/application/usecases"`, "_ = usecases.Queries{}", mcpAdapterAllowed, "usecases.Queries"},
		{"unit of work port", `"github.com/IQVO/product-discovery/internal/application/ports"`, "var _ ports.UnitOfWork", mcpAdapterAllowed, "ports.UnitOfWork"},
		{"unit of work adapter in cmd", `"github.com/IQVO/product-discovery/internal/adapters/outbound/postgres"`, "_ = postgres.NewUnitOfWork(nil)", mcpCmdAllowed, "postgres.NewUnitOfWork"},
		{"kafka driver", `"github.com/segmentio/kafka-go"`, "", mcpCmdAllowed, "kafka-go"},
		{"memory double", `"github.com/IQVO/product-discovery/internal/adapters/outbound/memory"`, "", mcpCmdAllowed, "outbound/memory"},
		{"rest adapter", `inboundhttp "github.com/IQVO/product-discovery/internal/adapters/inbound/http"`, "", mcpAdapterAllowed, "inbound/http"},
		{"read answers", `"github.com/IQVO/product-discovery/internal/application/usecases"`, "_ = usecases.SearchPage{}", mcpAdapterAllowed, ""},
		{"not-found sentinel", `"github.com/IQVO/product-discovery/internal/application/ports"`, "_ = ports.ErrNotFound", mcpAdapterAllowed, ""},
		{"read-side postgres", `"github.com/IQVO/product-discovery/internal/adapters/outbound/postgres"`, "_ = postgres.NewSearch(nil)", mcpCmdAllowed, ""},
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
}
