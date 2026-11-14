package mcp

import "testing"

// The authority ladder is the fleet's pinned declaration shape (Agentic
// Standard v2, AS-1): one const ladder and one helper in every repo. This
// surface is read-only, so only L1 is registered on a tool; the ladder is
// pinned here so a future L2..L4 tool starts from the same shape.
func TestAuthorityMetaCarriesTheLevelUnderTheFleetKey(t *testing.T) {
	if authorityMetaKey != "com.warehouse/authority" {
		t.Fatalf("authorityMetaKey = %q, want the fleet key com.warehouse/authority", authorityMetaKey)
	}
	for _, level := range []string{authorityL1, authorityL2, authorityL3, authorityL4} {
		meta := authorityMeta(level)
		if len(meta) != 1 || meta[authorityMetaKey] != level {
			t.Errorf("authorityMeta(%q) = %v, want exactly {%s: %s}", level, meta, authorityMetaKey, level)
		}
	}
	if authorityL1 != "L1" || authorityL2 != "L2" || authorityL3 != "L3" || authorityL4 != "L4" {
		t.Errorf("ladder = %s %s %s %s, want L1 L2 L3 L4", authorityL1, authorityL2, authorityL3, authorityL4)
	}
}
