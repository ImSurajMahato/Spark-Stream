package commands

import (
	"github.com/gotd/td/tg"
	"testing"
)

func TestDeliveredIDExactMapping(t *testing.T) {
	u := &tg.Updates{Updates: []tg.UpdateClass{&tg.UpdateMessageID{RandomID: 11, ID: 12}, &tg.UpdateMessageID{RandomID: 99, ID: 100}}}
	if deliveredID(u, 99) != 100 || deliveredID(u, 5) != 0 {
		t.Fatal("incorrect random-ID match")
	}
	c := &tg.UpdatesCombined{Updates: u.Updates}
	if deliveredID(c, 11) != 12 {
		t.Fatal("combined updates")
	}
	if deliveredID(&tg.UpdatesTooLong{}, 11) != 0 {
		t.Fatal("unknown updates should fail closed")
	}
}
