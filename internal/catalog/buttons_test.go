package catalog

import "testing"

func TestParseButton(t *testing.T) {
	for _, data := range []string{"s:1", "f:123", "s:2147483647"} {
		_, _, ok := ParseButton(data)
		if !ok {
			t.Fatalf("reject %s", data)
		}
	}
	for _, data := range []string{"s:0", "f:-1", "s:01", "s:+1", "s:2147483648", "s:1:2", "x:1", "f:abc", "", "s:12345678901234567890"} {
		_, _, ok := ParseButton(data)
		if ok {
			t.Fatalf("accept %s", data)
		}
	}
}
