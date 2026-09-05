package serverlog

import (
	"strings"
	"testing"
)

func TestUpsertMemberDerivesTheMatchedMember(t *testing.T) {
	// server_members stores no link to members. The match is derived from the
	// row's discord_user_id so it cannot go stale, and the column it replaced
	// must not come back.
	if strings.Contains(upsertMember, "member_id") {
		t.Fatal("upsert still references a member_id column")
	}
	if !strings.Contains(upsertMember, "SELECT m.id FROM members m WHERE m.user_id = u.discord_user_id") {
		t.Fatal("upsert does not derive the matched member from discord_user_id")
	}
	if !strings.Contains(findEvent, "SELECT m.id FROM members m WHERE m.user_id = sm.discord_user_id") {
		t.Fatal("duplicate lookup does not derive the matched member")
	}
	// Identity is the character plus the Steam account it played from. Discord
	// id and license are mutable, so neither can be part of the key.
	if !strings.Contains(upsertMember, "ON CONFLICT (cid, steamhex) DO UPDATE SET") {
		t.Fatal("identity is not keyed on (cid, steamhex)")
	}
	for _, keyColumn := range []string{"cid             = EXCLUDED.cid", "steamhex        = "} {
		if strings.Contains(upsertMember, keyColumn) {
			t.Fatalf("%q is part of the key and must not be overwritten", keyColumn)
		}
	}
	// license_id is data now, not identity, so it follows the latest event.
	if !strings.Contains(upsertMember, "license_id      = EXCLUDED.license_id") {
		t.Fatal("license_id is not updated from the event")
	}
}

func TestDuplicateCheckUsesTransactionEventLock(t *testing.T) {
	if !strings.Contains(lockEvent, "pg_advisory_xact_lock") {
		t.Fatal("event lock is not transaction-scoped")
	}
	if !strings.Contains(findEvent, "WHERE sl.payload = $1::jsonb") {
		t.Fatal("duplicate lookup does not use the payload")
	}
	// Idempotency now rests on the payload column, so the insert must decline a
	// body it already holds rather than append a second row.
	if !strings.Contains(insertLog, "ON CONFLICT (payload) DO NOTHING") {
		t.Fatal("insert is not idempotent on payload")
	}
}
