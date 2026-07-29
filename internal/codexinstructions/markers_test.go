package codexinstructions

import "testing"

func TestParseModelDefaultMarkers(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		private bool
	}{
		{"gpt-5.6-sol", "gpt-5.6-sol", false},
		{"private/gpt-5.6-sol", "gpt-5.6-sol", true},
		{"gpt-5.6-sol-private", "gpt-5.6-sol", true},
		{"private/gpt-5.6-sol-private", "gpt-5.6-sol", true},
		{"PRIVATE/gpt-5.6-sol", "PRIVATE/gpt-5.6-sol", false},
	}
	for _, tc := range cases {
		got, private := ParseModel(tc.in, DefaultMarkers())
		if got != tc.want || private != tc.private {
			t.Fatalf("ParseModel(%q) = %q,%v want %q,%v", tc.in, got, private, tc.want, tc.private)
		}
	}
}

func TestAuthAllows(t *testing.T) {
	if AuthAllows(nil, nil) {
		t.Fatal("nil auth should not allow")
	}
	if !AuthAllows(nil, map[string]any{AuthMetadataKey: true}) {
		t.Fatal("metadata true should allow")
	}
	if !AuthAllows(map[string]string{AuthAttributeKey: "true"}, nil) {
		t.Fatal("attribute true should allow")
	}
	if AuthAllows(nil, map[string]any{AuthMetadataKey: false}) {
		t.Fatal("false should not allow")
	}
}

func TestModelMatches(t *testing.T) {
	if !ModelMatches([]string{"gpt-5*"}, "gpt-5.5") {
		t.Fatal("expected gpt-5.5 to match gpt-5*")
	}
	if !ModelMatches([]string{"gpt-5*"}, "kiro/gpt-5.6-luna") {
		t.Fatal("expected provider-prefixed gpt-5 model to match gpt-5*")
	}
	if ModelMatches([]string{"gpt-5*"}, "grok-4.5") {
		t.Fatal("did not expect grok-4.5 to match gpt-5*")
	}
}

func TestVirtualModelIDs(t *testing.T) {
	ids := VirtualModelIDs("gpt-5.5", DefaultMarkers())
	want := map[string]bool{"private/gpt-5.5": true, "gpt-5.5-private": true}
	if len(ids) != len(want) {
		t.Fatalf("VirtualModelIDs len = %d (%v), want %d", len(ids), ids, len(want))
	}
	for _, id := range ids {
		if !want[id] {
			t.Fatalf("unexpected virtual id %q", id)
		}
	}
	if got := VirtualModelIDs("private/gpt-5.5", DefaultMarkers()); len(got) != 0 {
		t.Fatalf("already-private model should not expand, got %v", got)
	}
}
