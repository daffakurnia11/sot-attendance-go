package router

import "testing"

func TestRouterMatch(t *testing.T) {
	t.Parallel()

	router := NewRouter("!")
	tests := []struct {
		content string
		want    string
	}{
		{content: "!recap", want: "recap"},
		{content: "!check", want: "check"},
		{content: "!check <@123456789>", want: "check"},
		{content: "!check <@!123456789>", want: "check"},
		{content: "!craft vector:30 mp9:20", want: "craft"},
		{content: "!money balance", want: "money"},
		{content: "!money deposit 1000 income", want: "money"},
		{content: "!Me"},
		{content: "!check extra"},
		{content: "!check <@member>"},
		{content: "!check <@123> extra"},
		{content: "hello"},
	}
	for _, tt := range tests {
		if got := router.Match(tt.content); got != tt.want {
			t.Errorf("Match(%q) = %q, want %q", tt.content, got, tt.want)
		}
	}
}
