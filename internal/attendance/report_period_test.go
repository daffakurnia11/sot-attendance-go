package attendance

import (
	"testing"
	"time"
)

func TestContractBoundaryClampsToMonthEnd(t *testing.T) {
	location := time.FixedZone("Asia/Jakarta", 7*60*60)
	got := contractBoundary(2027, time.February, 31, location)
	if want := "2027-02-28"; got.Format("2006-01-02") != want {
		t.Fatalf("contractBoundary() = %s, want %s", got.Format("2006-01-02"), want)
	}
}
