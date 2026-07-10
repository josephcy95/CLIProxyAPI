package auth

import "testing"

func TestAuthMatchesPrivateInstructionsPolicy(t *testing.T) {
	marked := &Auth{Provider: "codex", Attributes: map[string]string{"allow_private_instructions": "true"}}
	unmarked := &Auth{Provider: "codex"}
	other := &Auth{Provider: "claude"}

	if !authMatchesPrivateInstructionsPolicy(unmarked, false, true, false) {
		t.Fatal("unmarked should serve normal requests")
	}
	if !authMatchesPrivateInstructionsPolicy(marked, false, true, false) {
		t.Fatal("marked should serve normal requests when reserve is off")
	}
	if authMatchesPrivateInstructionsPolicy(marked, false, true, true) {
		t.Fatal("marked should be reserved when reserve is on")
	}
	if authMatchesPrivateInstructionsPolicy(unmarked, true, true, false) {
		t.Fatal("unmarked should not serve private requests when require-allow")
	}
	if !authMatchesPrivateInstructionsPolicy(marked, true, true, false) {
		t.Fatal("marked should serve private requests when require-allow")
	}
	if !authMatchesPrivateInstructionsPolicy(unmarked, true, false, false) {
		t.Fatal("unmarked may serve private requests when require-allow is false")
	}
	if !authMatchesPrivateInstructionsPolicy(other, false, true, true) {
		t.Fatal("non-codex should remain eligible for normal traffic")
	}
}
