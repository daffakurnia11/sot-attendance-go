package business

import "testing"

func TestUpdateValidation(t *testing.T) {
	valid := Update{ID: 1, Direction: "sale", Version: 1, Values: Values{Price: 5000, Unit: "piece"}}
	if !valid.Valid() {
		t.Fatal("valid update rejected")
	}
	for _, change := range []func(*Update){func(u *Update) { u.Price = -1 }, func(u *Update) { u.Price = 9007199254740992 }, func(u *Update) { u.Unit = " " }, func(u *Update) { u.Version = 0 }, func(u *Update) { u.Direction = "business_partner_prices;DROP TABLE members" }, func(u *Update) { u.ID = 0 }} {
		u := valid
		change(&u)
		if u.Valid() {
			t.Fatalf("accepted %+v", u)
		}
	}
}
