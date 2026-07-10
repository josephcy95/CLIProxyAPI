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
