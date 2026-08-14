package router

import "strings"

type Router struct {
	prefix string
}

func NewRouter(prefix string) *Router {
	return &Router{prefix: prefix}
}

func (r *Router) Match(content string) string {
	switch strings.TrimSpace(content) {
	case r.prefix + "me":
		return "me"
	case r.prefix + "recap":
		return "recap"
	case r.prefix + "attendance-start":
		return "attendance-start"
	case r.prefix + "attendance-end":
		return "attendance-end"
	}
	return ""
}
