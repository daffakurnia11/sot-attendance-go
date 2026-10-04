package payslip

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roleTransport func(*http.Request) (*http.Response, error)

func (f roleTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestGuildRolesResolveNamesAndGuestOverridesOtherRoles(t *testing.T) {
	client := &http.Client{Transport: roleTransport(func(request *http.Request) (*http.Response, error) {
		body := `[{"id":"1526876863329341530","name":"SOT CR"},{"id":"1554058898028101696","name":"SOT CR Guest"},{"id":"guild","name":"@everyone"},{"id":"other","name":"UNDERBOSS"}]`
		if strings.Contains(request.URL.Path, "/members") {
			body = `[{"user":{"id":"111"},"roles":["other","1526876863329341530"]},{"user":{"id":"222"},"roles":["1526876863329341530","other","1554058898028101696"]}]`
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	reader, err := NewGuildRoleReader(client, "test-token", "guild", []string{"1554058898028101696"})
	if err != nil {
		t.Fatal(err)
	}
	roles, err := reader.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(roles["111"].Roles, ",") != "SOT CR" || roles["111"].Excluded || !roles["222"].Excluded || strings.Join(roles["222"].Roles, ",") != "SOT CR,SOT CR Guest" {
		t.Fatalf("roles = %+v", roles)
	}
}

func TestGuildRoleReaderRejectsUpstreamFailure(t *testing.T) {
	client := &http.Client{Transport: roleTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 403, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"message":"Missing Permissions"}`))}, nil
	})}
	reader, _ := NewGuildRoleReader(client, "test-token", "guild", nil)
	if _, err := reader.Load(context.Background()); err == nil {
		t.Fatal("expected role lookup failure")
	}
}
