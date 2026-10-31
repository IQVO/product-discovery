package a2a

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
)

const testAgentURL = "http://product-discovery-a2a.warehouse-systems.svc.cluster.local:8093/"

// TestCardShape pins the identity, interface and capability surface of the
// Agent Card: the discovery contract other contexts program against.
func TestCardShape(t *testing.T) {
	card := Card(Config{AgentURL: testAgentURL, Version: "1.0.0"})

	if card.Name != "product-discovery" {
		t.Errorf("card name = %q, want product-discovery", card.Name)
	}
	if len(card.SupportedInterfaces) != 1 {
		t.Fatalf("card has %d interfaces, want 1", len(card.SupportedInterfaces))
	}
	iface := card.SupportedInterfaces[0]
	if iface.URL != testAgentURL || iface.ProtocolBinding != a2a.TransportProtocolJSONRPC {
		t.Errorf("interface = %+v, want JSONRPC at the configured agent URL", iface)
	}
	if card.Capabilities.Streaming {
		t.Error("streaming must not be advertised: this is a message-only agent")
	}
	if card.SecuritySchemes != nil || card.SecurityRequirements != nil {
		t.Error("card must declare no security schemes: the surface is unauthenticated fleet-wide")
	}
	if card.Provider == nil || card.Provider.Org != "IQVO" {
		t.Errorf("provider = %+v, want IQVO", card.Provider)
	}
}

// TestCardSkillsAreCuratedAndTruthful pins the two-skill, READ-ONLY surface
// and proves the card never lies about its own contract: every advertised
// example must parse under the SAME strict decoder the executor uses, name its
// own skill, and carry arguments that skill accepts.
func TestCardSkillsAreCuratedAndTruthful(t *testing.T) {
	card := Card(Config{AgentURL: testAgentURL, Version: "1.0.0"})

	want := map[string]bool{skillSearchProducts: true, skillGetProduct: true}
	if len(card.Skills) != len(want) {
		t.Fatalf("card has %d skills, want exactly %d (curated, goal-shaped, not one per endpoint)", len(card.Skills), len(want))
	}
	for _, s := range card.Skills {
		if !want[s.ID] {
			t.Errorf("unexpected skill %q on the card", s.ID)
			continue
		}
		if s.Name == "" || s.Description == "" || len(s.Examples) == 0 {
			t.Errorf("skill %q lacks a name, description or example", s.ID)
		}
		if !containsTag(s.Tags, "read") || containsTag(s.Tags, "write") {
			t.Errorf("skill %q must be tagged read and never write, tags = %v", s.ID, s.Tags)
		}
		for _, ex := range s.Examples {
			assertExampleAccepted(t, s.ID, ex)
		}
	}
}

func assertExampleAccepted(t *testing.T, skill, example string) {
	t.Helper()
	var req skillRequest
	if err := decodeStrict([]byte(example), &req); err != nil {
		t.Errorf("skill %q example is not valid under the strict decoder: %v", skill, err)
		return
	}
	if req.Skill != skill {
		t.Errorf("skill %q example names skill %q", skill, req.Skill)
	}
	var err error
	switch skill {
	case skillSearchProducts:
		err = decodeStrict(req.Arguments, &searchProductsArgs{})
	case skillGetProduct:
		err = decodeStrict(req.Arguments, &getProductArgs{})
	}
	if err != nil {
		t.Errorf("skill %q example arguments are rejected by its own strict decoder: %v", skill, err)
	}
}

func containsTag(tags []string, want string) bool {
	for _, tag := range tags {
		if tag == want {
			return true
		}
	}
	return false
}

// TestCardSkillsMatchTheDispatchTable keeps the card and the executor in step:
// every advertised skill is dispatched, and nothing un-advertised is.
func TestCardSkillsMatchTheDispatchTable(t *testing.T) {
	card := Card(Config{AgentURL: testAgentURL, Version: "1.0.0"})
	if len(card.Skills) != len(skills) {
		t.Fatalf("card advertises %d skills, the dispatch table serves %d", len(card.Skills), len(skills))
	}
	for _, s := range card.Skills {
		if _, ok := skills[s.ID]; !ok {
			t.Errorf("card skill %q is not dispatched", s.ID)
		}
	}
}

// TestCardNeverClaimsToWrite keeps the card honest about the read-only charter:
// no skill id may suggest it changes the catalogue, and the description says so.
func TestCardNeverClaimsToWrite(t *testing.T) {
	card := Card(Config{AgentURL: testAgentURL, Version: "1.0.0"})
	for _, s := range card.Skills {
		for _, verb := range []string{"create_", "update_", "set_", "publish_", "retire_", "withdraw_", "place_", "reserve_"} {
			if strings.HasPrefix(s.ID, verb) {
				t.Errorf("skill %q looks like a write; this agent is read-only", s.ID)
			}
		}
	}
	if !strings.Contains(card.Description, "read-only") || !strings.Contains(card.Description, "never changes") {
		t.Errorf("the card description must say the agent is read-only: %q", card.Description)
	}
}

// TestCardSerializable proves the card marshals to JSON a client can parse
// back, with the discovery fields intact.
func TestCardSerializable(t *testing.T) {
	raw, err := json.Marshal(Card(Config{AgentURL: testAgentURL, Version: "1.0.0"}))
	if err != nil {
		t.Fatalf("marshal card: %v", err)
	}
	var back a2a.AgentCard
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("card JSON does not round-trip: %v", err)
	}
	if back.Name != "product-discovery" || len(back.Skills) != 2 {
		t.Errorf("round-tripped card = %q with %d skills", back.Name, len(back.Skills))
	}
}

// TestSkillExamplesRunOverTheWire executes every advertised example against a
// seeded agent: an example that answered a problem would be a lie on the card.
func TestSkillExamplesRunOverTheWire(t *testing.T) {
	w := newWire(t, newFixture(t).deps())
	for _, s := range Card(Config{AgentURL: testAgentURL}).Skills {
		for _, ex := range s.Examples {
			var req map[string]any
			if err := json.Unmarshal([]byte(ex), &req); err != nil {
				t.Fatal(err)
			}
			if env, _ := w.call(t, a2a.NewDataPart(req)); env.Problem != nil || env.Data == nil {
				t.Errorf("example %s answered %+v", ex, env)
			}
		}
	}
}
