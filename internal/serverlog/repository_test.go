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
	// One Rockstar account can hold several characters, so identity is keyed on
	// the character, not the account.
	if !strings.Contains(upsertMember, "ON CONFLICT (license_id, cid) DO UPDATE SET") {
		t.Fatal("identity is not keyed on (license_id, cid)")
	}
	if strings.Contains(upsertMember, "cid             = EXCLUDED.cid") {
		t.Fatal("cid is part of the key and must not be overwritten")
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
