package selfupdate

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestResolveLatest_TagFormat verifies ResolveLatest anchors the shape of the
// tag_name it returns: a valid semver tag passes, a malformed/hostile value
// (e.g. a path-traversal string) is rejected with an error.
func TestResolveLatest_TagFormat(t *testing.T) {
	cases := []struct {
		name    string
		tag     string
		wantErr bool
	}{
		{"plain semver", "v0.3.0", false},
		{"no-v semver", "1.2.3", false},
		{"path traversal", "../../etc", true},
		{"garbage", "not-a-tag", true},
		{"empty", "", true},
		{"embedded path", "v0.3.0/../../evil", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/releases/latest") {
					fmt.Fprintf(w, `{"tag_name":%q}`, c.tag)
					return
				}
				http.NotFound(w, r)
			}))
			defer srv.Close()
			old := APIBase
			APIBase = srv.URL
			defer func() { APIBase = old }()

			tag, err := ResolveLatest(context.Background())
			if c.wantErr {
				if err == nil {
					t.Fatalf("tag %q: expected error, got tag %q", c.tag, tag)
				}
				return
			}
			if err != nil {
				t.Fatalf("tag %q: unexpected error: %v", c.tag, err)
			}
			if tag != c.tag {
				t.Errorf("tag = %q, want %q", tag, c.tag)
			}
		})
	}
}
