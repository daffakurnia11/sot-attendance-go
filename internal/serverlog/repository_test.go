package serverlog

import (
	"strings"
	"testing"
)

func TestUpsertMemberResolvesMemberOnInitialInsert(t *testing.T) {
	if !strings.Contains(upsertMember, "INSERT INTO server_members (license_id, member_id") {
		t.Fatal("initial insert does not populate member_id")
	}
	if !strings.Contains(upsertMember, "SELECT m.id FROM members m WHERE m.user_id = $2") {
		t.Fatal("initial insert does not resolve the Discord user")
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
