package catalog

import "testing"

func TestSlugFromFilename(t *testing.T) {
	cases := []struct {
		in      string
		slug    string
		mp4, ok bool
	}{
		{"6a1af7e634ab08907f65d4b7.mp4", "6a1af7e634ab08907f65d4b7", true, true},
		{"physics-01.MP4", "physics-01", true, true},
		{"a.b.mp4", "a.b", true, false},
		{"lecture.mkv", "", false, false},
		{".mp4", "", true, false},
		{"mp4", "", false, false},
		{"my lecture.mp4", "my lecture", true, false},
	}
	for _, c := range cases {
		slug, mp4, ok := SlugFromFilename(c.in)
		if mp4 != c.mp4 || ok != c.ok || (ok && slug != c.slug) {
			t.Fatalf("%q: got %q %v %v", c.in, slug, mp4, ok)
		}
	}
}

func TestItoa(t *testing.T) {
	for in, want := range map[int64]string{0: "0", 7: "7", 1002345: "1002345", -5: "-5"} {
		if itoa(in) != want {
			t.Fatal(in)
		}
	}
}
