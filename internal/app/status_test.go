package app

import "testing"

func TestShortServerName(t *testing.T) {
	t.Parallel()
	if got := shortServerName("CR Roleplay"); got != "CR" {
		t.Fatalf("shortServerName() = %q", got)
	}
}
