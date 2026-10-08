package catalog

import (
	"testing"
	"time"
)

func TestDeliveryPolicy(t *testing.T) {
	now := time.Unix(100000, 123456789)
	d := NewDelivery("delivery_1", 7513979260, 123, 1234, 99, 42, "0123456789abcdef0123456789abcdef", now)
	if !d.Valid() || d.DeleteAt.Sub(d.CreatedAt) != 4*time.Hour || d.CreatedAt.Nanosecond()%1000000 != 0 {
		t.Fatal(d)
	}
	d.State = "sent"
	if d.Valid() {
		t.Fatal("sent without ID accepted")
	}
	d.MessageID = 1
	if !d.Valid() {
		t.Fatal("valid sent rejected")
	}
	d.DeleteAt = d.DeleteAt.Add(time.Second)
	if d.Valid() {
		t.Fatal("wrong TTL accepted")
	}
}
func TestDeliveryRetryBound(t *testing.T) {
	if RetryDelay(0) != time.Minute || RetryDelay(1) != 2*time.Minute || RetryDelay(10000) != 32*time.Minute {
		t.Fatal("retry bounds")
	}
}
