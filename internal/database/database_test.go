package database

import "testing"

func TestSkipMigrations(t *testing.T) {
	// Anything a person would plausibly write to mean yes, so pointing a local
	// build at somebody else's database does not silently migrate it.
	for _, value := range []string{"1", "true", "TRUE", "Yes", " on ", "True"} {
		if !SkipMigrations(value) {
			t.Errorf("SkipMigrations(%q) = false, want true", value)
		}
	}
	// Everything else migrates, so the default and the deployed config keep
	// applying migrations at startup.
	for _, value := range []string{"", "0", "false", "no", "off", "maybe", "  "} {
		if SkipMigrations(value) {
			t.Errorf("SkipMigrations(%q) = true, want false", value)
		}
	}
}
