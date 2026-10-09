package store

import "testing"

func TestLikePattern(t *testing.T) {
	s := func(v string) *string { return &v }
	cases := []struct {
		in   *string
		want *string
	}{
		{nil, nil},
		{s("  "), nil},
		{s("Иван"), s("%Иван%")},
		{s(" 50%_a\\ "), s(`%50\%\_a\\%`)},
	}
	for _, tc := range cases {
		got := likePattern(tc.in)
		if (got == nil) != (tc.want == nil) || (got != nil && *got != *tc.want) {
			t.Errorf("likePattern(%v) = %v, want %v", deref(tc.in), deref(got), deref(tc.want))
		}
	}
}

func deref(p *string) string {
	if p == nil {
		return "<nil>"
	}
	return *p
}

func TestExactPattern(t *testing.T) {
	if exactPattern(nil) != nil || exactPattern(ptr(" ")) != nil {
		t.Error("empty input must give nil")
	}
	if got := *exactPattern(ptr(" 5_% ")); got != `5\_\%` {
		t.Errorf("exactPattern = %q", got)
	}
}
