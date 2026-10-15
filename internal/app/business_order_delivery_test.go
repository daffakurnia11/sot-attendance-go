package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type orderTestRow func(...any) error

func (r orderTestRow) Scan(v ...any) error { return r(v...) }

type orderTestTx struct {
	pgx.Tx
	row       func(string, ...any) pgx.Row
	exec      func(string, ...any) (pgconn.CommandTag, error)
	commitErr error
}

func (t *orderTestTx) QueryRow(_ context.Context, q string, args ...any) pgx.Row {
	return t.row(q, args...)
}
func (t *orderTestTx) Exec(_ context.Context, q string, args ...any) (pgconn.CommandTag, error) {
	return t.exec(q, args...)
}
func (t *orderTestTx) Commit(context.Context) error { return t.commitErr }

type orderTestTransport func(*http.Request) (*http.Response, error)

func (f orderTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestOrderNotificationRetriesOnlyFailedDestination(t *testing.T) {
	type notice struct {
		destination, message string
		deferred             bool
	}
	notices := []*notice{{destination: "office"}, {destination: "boss"}}
	var selected *notice
	tx := &orderTestTx{}
	tx.row = func(q string, _ ...any) pgx.Row {
		if !strings.Contains(q, "delivered_message_id IS NULL") || !strings.Contains(q, "SKIP LOCKED") {
			t.Fatal("delivery must claim only pending rows")
		}
		return orderTestRow(func(v ...any) error {
			for _, n := range notices {
				if n.message == "" && !n.deferred {
					selected = n
					*v[0].(*int64) = 42
					*v[1].(*string) = n.destination
					*v[2].(*[]byte) = []byte(`{"title":"Settled snapshot"}`)
					return nil
				}
			}
			return pgx.ErrNoRows
		})
	}
	tx.exec = func(q string, args ...any) (pgconn.CommandTag, error) {
		if !strings.HasPrefix(q, "UPDATE business_order_notifications") {
			t.Fatalf("delivery wrote settlement: %s", q)
		}
		if strings.Contains(q, "delivered_message_id=$3") {
			selected.message = args[2].(string)
		} else {
			selected.deferred = true
		}
		return pgconn.NewCommandTag("UPDATE 1"), nil
	}
	calls := map[string]int{}
	failBoss := true
	session, _ := discordgo.New("Bot test")
	session.Client = &http.Client{Transport: orderTestTransport(func(r *http.Request) (*http.Response, error) {
		channel := "office"
		if strings.Contains(r.URL.Path, "/boss/") {
			channel = "boss"
		}
		calls[channel]++
		var body discordgo.MessageSend
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if len(body.Embeds) != 1 || body.Embeds[0].Title != "Settled snapshot" || body.AllowedMentions == nil {
			t.Fatal("incorrect snapshot or mentions")
		}
		status, payload := 200, `{"id":"sent-`+channel+`"}`
		if channel == "boss" && failBoss {
			status = 403
			payload = `{"message":"Missing Permissions","code":50013}`
		}
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(payload)), Request: r}, nil
	})}
	b := &Bot{officeMoneyChannelID: "office", stashChannels: stashChannels{boss: "boss"}, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	attempt := func(want bool) {
		t.Helper()
		found, err := b.deliverOrderNotification(context.Background(), tx, session)
		if err != nil || found != want {
			t.Fatalf("found=%v err=%v", found, err)
		}
	}
	attempt(true)
	attempt(true)
	attempt(false)
	if notices[0].message == "" || notices[1].message != "" || !notices[1].deferred {
		t.Fatal("partial delivery not retained")
	}
	// A fresh session simulates restart; queue state belongs to the database.
	client := session.Client
	session, _ = discordgo.New("Bot test")
	session.Client = client
	failBoss = false
	notices[1].deferred = false
	attempt(true)
	attempt(false)
	if calls["office"] != 1 || calls["boss"] != 2 || notices[1].message != "sent-boss" {
		t.Fatalf("unexpected deliveries: %v", calls)
	}
}

func TestOrderCommitUncertaintyKeepsIDAndCannotInsertAgain(t *testing.T) {
	inserts := 0
	tx := &orderTestTx{commitErr: errors.New("connection lost during commit")}
	tx.row = func(_ string, _ ...any) pgx.Row {
		inserts++
		return orderTestRow(func(v ...any) error {
			*v[0].(*int64) = 42
			*v[1].(*time.Time) = time.Now()
			*v[2].(*time.Time) = time.Now()
			return nil
		})
	}
	tx.exec = func(string, ...any) (pgconn.CommandTag, error) { return pgconn.NewCommandTag("INSERT 1"), nil }
	d := &orderDraft{Lines: []orderLine{{Quantity: 1}}}
	if err := saveBusinessOrder(context.Background(), tx, d, 1); err == nil || d.OrderID != 42 {
		t.Fatal("lost ambiguous saved ID")
	}
	if err := saveBusinessOrder(context.Background(), tx, d, 1); err != nil || inserts != 1 {
		t.Fatal("retry inserted another order")
	}
	// An error before COMMIT is a known failed save and can be retried.
	d.OrderID = 0
	tx.exec = func(string, ...any) (pgconn.CommandTag, error) {
		return pgconn.CommandTag{}, errors.New("item insert failed")
	}
	if err := saveBusinessOrder(context.Background(), tx, d, 1); err == nil || d.OrderID != 0 {
		t.Fatal("failed insert kept saved ID")
	}
}

func TestBusyOrderDoesNotBlockOtherDrafts(t *testing.T) {
	first, second := &orderDraft{mu: &sync.Mutex{}}, &orderDraft{mu: &sync.Mutex{}}
	b := &Bot{orderDrafts: map[string]*orderDraft{"first": first, "second": second}}
	first.mu.Lock()
	defer first.mu.Unlock()
	if b.lockOrderDraft("first") != nil {
		t.Fatal("busy draft acquired")
	}
	d := b.lockOrderDraft("second")
	if d == nil {
		t.Fatal("other draft blocked")
	}
	b.deleteOrderDraft("second", d)
	d.mu.Unlock()
	if b.lockOrderDraft("second") != nil || !d.Closed {
		t.Fatal("closed draft remains available")
	}
}
