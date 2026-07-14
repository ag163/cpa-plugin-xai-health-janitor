package main

import (
	"testing"
	"time"
)

func TestClassifyRecordedStatus(t *testing.T) {
	cases := map[string]string{
		"unauthorized":      "auth_invalid",
		"payment_required":  "spending_limit",
		"quota exhausted":   "rate_limited",
		"permission-denied": "permission_denied",
	}
	for message, want := range cases {
		if got := classifyRecordedStatus("error", message); got != want {
			t.Fatalf("classifyRecordedStatus(%q) = %q, want %q", message, got, want)
		}
	}
}

func TestHardFailureConfirmationRequiresNewEvent(t *testing.T) {
	hardFailureMu.Lock()
	hardFailures = make(map[string]hardFailureState)
	hardFailureMu.Unlock()

	first := time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC)
	if confirmHardFailure("xai-test.json", "auth_invalid", first, 2) {
		t.Fatal("first failure must not confirm deletion")
	}
	if confirmHardFailure("xai-test.json", "auth_invalid", first, 2) {
		t.Fatal("same failure event must not count twice")
	}
	if !confirmHardFailure("xai-test.json", "auth_invalid", first.Add(time.Minute), 2) {
		t.Fatal("newer failure event must confirm deletion")
	}
}
