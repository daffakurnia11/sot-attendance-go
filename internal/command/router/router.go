package router

import "strings"

const (
	CraftCommand = "craft"
	MoneyCommand = "money"
)

type Router struct {
	prefix string
}

func NewRouter(prefix string) *Router {
	return &Router{prefix: prefix}
}

func (r *Router) Prefix() string { return r.prefix }

func (r *Router) Match(content string) string {
	trimmed := strings.TrimSpace(content)
	switch trimmed {
	case r.prefix + "recap":
		return "recap"
	case r.prefix + "check":
		return "check"
	}
	parts := strings.Fields(trimmed)
	if len(parts) >= 2 && parts[0] == r.prefix+CraftCommand {
		return CraftCommand
	}
	if len(parts) >= 2 && parts[0] == r.prefix+MoneyCommand {
		return MoneyCommand
	}
	if len(parts) == 2 && parts[0] == r.prefix+"check" && isUserMention(parts[1]) {
		return "check"
	}
	return ""
}

func isUserMention(value string) bool {
	if !strings.HasPrefix(value, "<@") || !strings.HasSuffix(value, ">") {
		return false
	}
	id := strings.TrimSuffix(strings.TrimPrefix(value, "<@"), ">")
	id = strings.TrimPrefix(id, "!")
	if id == "" {
		return false
	}
	for _, character := range id {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}
