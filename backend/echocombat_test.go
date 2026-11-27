package main

import "testing"

// Real cases from the ECGL roster vs the echocombat.com archive.
func TestEchoMatchScore(t *testing.T) {
	cases := []struct {
		ours     string
		upstream string
		min      int
		max      int
	}{
		// our stored display name for Alpha-Phoenix
		{"Alpha-Phoenix (10) [51.77%]", "Alpha-Phoenix", 85, 85},
		{"Alpha-Phoenix", "Alpha-Phoenix", 100, 100},
		{"alpha_phoenix", "Alpha-Phoenix", 100, 100},
		{"AlphaPhoenix", "Alpha-Phoenix", 100, 100},
		{"🙏FatherHungis🙏", "FatherHungis", 100, 100},
		{"minileo99", "minileo99 (the forgotten", 90, 90},
		// must NOT match a different player
		{"psycho-", "psycho1144", 0, 0},
		{"", "Anything", 0, 0},
	}

	for _, c := range cases {
		got := echoMatchScore(c.ours, c.upstream)
		if got < c.min || got > c.max {
			t.Errorf("echoMatchScore(%q, %q) = %d, want %d..%d", c.ours, c.upstream, got, c.min, c.max)
		}
	}
}

func TestEchoQueryVariants(t *testing.T) {
	variants := echoQueryVariants("Alpha-Phoenix (10) [51.77%]")
	want := map[string]bool{"phoenix": true, "alpha": true}
	found := map[string]bool{}
	for _, v := range variants {
		found[toLowerASCII(v)] = true
	}
	for w := range want {
		if !found[w] {
			t.Errorf("echoQueryVariants(%s) = %v, missing %s", "Alpha-Phoenix (10) [51.77%]", variants, w)
		}
	}
}

func toLowerASCII(s string) string {
	out := []byte(s)
	for i := range out {
		if out[i] >= 'A' && out[i] <= 'Z' {
			out[i] += 'a' - 'A'
		}
	}
	return string(out)
}
