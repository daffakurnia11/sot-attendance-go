package router

import "testing"

func TestRouterMatch(t *testing.T) {
	t.Parallel()

	router := NewRouter("!")
	tests := []struct {
		content string
		want    string
	}{
		{content: "!me", want: "me"},
		{content: "  !me  ", want: "me"},
		{content: "!recap", want: "recap"},
		{content: "!attendance-start", want: "attendance-start"},
		{content: "!attendance-end", want: "attendance-end"},
		{content: "!Me"},
		{content: "!me extra"},
		{content: "hello"},
	}
	for _, tt := range tests {
		if got := router.Match(tt.content); got != tt.want {
			t.Errorf("Match(%q) = %q, want %q", tt.content, got, tt.want)
		}
	}
}
