package catalog

import (
	"strings"
	"testing"
)

func TestSlugs(t *testing.T) {
	for _, s := range []string{"6a2698f9735cb5428a449a0a", "physics-01", "A_b", strings.Repeat("x", 64)} {
		if !ValidSlug(s) {
			t.Fatal(s)
		}
	}
	for _, s := range []string{"", "a b", "a/b", "%2f", "हिंदी", strings.Repeat("x", 65)} {
		if ValidSlug(s) {
			t.Fatal(s)
		}
	}
	seen := map[string]bool{}
	for i := 0; i < 1000; i++ {
		s, e := RandomSlug()
		if e != nil || len(s) != 24 || !ValidSlug(s) || seen[s] {
			t.Fatal(s, e)
		}
		seen[s] = true
	}
}
func TestEntry(t *testing.T) {
	e := Entry{Slug: "lesson", ChannelID: 123, MessageID: 1, Hash: strings.Repeat("a", 32)}
	if !e.Valid() {
		t.Fatal(e)
	}
	e.Hash = strings.Repeat("z", 32)
	if e.Valid() {
		t.Fatal("nonhex")
	}
}
