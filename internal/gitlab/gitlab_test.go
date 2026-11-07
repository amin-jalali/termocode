package gitlab

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The fake token is split so no scanner sees a real-looking one.
var testToken = "glpat-" + "test0123456789abcdef"

func TestFromWebBase(t *testing.T) {
	for _, c := range []struct {
		base, host, path string
		ok               bool
	}{
		{"https://gitlab.com/group/sub/proj", "gitlab.com", "group/sub/proj", true},
		{"https://gitlab.example.org/team/app", "gitlab.example.org", "team/app", true},
		{"https://github.com/me/app", "", "", false},
		{"https://gitlab.com/", "", "", false},
		{"not a url", "", "", false},
	} {
		p, ok := FromWebBase(c.base)
		if ok != c.ok || p.Host != c.host || p.Path != c.path {
			t.Errorf("%q: %+v %v", c.base, p, ok)
		}
	}
}

func TestListMRs(t *testing.T) {
	var gotPath, gotToken, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotToken, gotQuery = r.URL.EscapedPath(), r.Header.Get("PRIVATE-TOKEN"), r.URL.RawQuery
		_, _ = io.WriteString(w, `[{"iid":12,"title":"Add login","author":{"username":"ann"},"source_branch":"login","target_branch":"main","web_url":"https://gitlab.com/g/p/-/merge_requests/12","state":"opened","draft":true},
{"iid":9,"title":"Fix typo","author":{"username":"bo"},"source_branch":"typo","target_branch":"main","web_url":"https://gitlab.com/g/p/-/merge_requests/9","state":"opened","draft":false}]`)
	}))
	defer srv.Close()
	mrs, err := New(srv.URL, testToken).ListMRs(context.Background(), "g/sub/p")
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/v4/projects/g%2Fsub%2Fp/merge_requests" {
		t.Errorf("path %q", gotPath)
	}
	if !strings.Contains(gotQuery, "state=opened") || gotToken != testToken {
		t.Errorf("query %q token sent %v", gotQuery, gotToken == testToken)
	}
	if len(mrs) != 2 || mrs[0].IID != 12 || mrs[0].Author != "ann" || !mrs[0].Draft || mrs[0].Source != "login" ||
		mrs[0].Target != "main" || mrs[1].URL != "https://gitlab.com/g/p/-/merge_requests/9" || mrs[1].Draft {
		t.Fatalf("mrs %+v", mrs)
	}
}

func TestCreateMR(t *testing.T) {
	var body map[string]any
	var method, path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.EscapedPath()
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"iid":13,"title":"Draft: Add login","source_branch":"login","target_branch":"main","web_url":"https://gitlab.com/g/p/-/merge_requests/13","draft":true}`)
	}))
	defer srv.Close()
	mr, err := New(srv.URL, testToken).CreateMR(context.Background(), "g/p", NewMR{Source: "login", Target: "main", Title: "Add login", Draft: true})
	if err != nil {
		t.Fatal(err)
	}
	if method != http.MethodPost || path != "/api/v4/projects/g%2Fp/merge_requests" {
		t.Errorf("%s %s", method, path)
	}
	if body["source_branch"] != "login" || body["target_branch"] != "main" || body["title"] != "Draft: Add login" {
		t.Errorf("body %v", body)
	}
	if mr.IID != 13 || mr.URL != "https://gitlab.com/g/p/-/merge_requests/13" {
		t.Errorf("mr %+v", mr)
	}
}

// Errors: 401 is ErrUnauthorized, 404 ErrNotFound, else GitLab's own
// message; the token is never in an error.
func TestErrors(t *testing.T) {
	status, answer := 0, ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = io.WriteString(w, answer)
	}))
	defer srv.Close()
	c := New(srv.URL, testToken)
	status, answer = 401, `{"message":"401 Unauthorized"}`
	if _, err := c.ListMRs(context.Background(), "g/p"); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("401: %v", err)
	}
	status, answer = 404, `{"message":"404 Project Not Found"}`
	if _, err := c.ListMRs(context.Background(), "g/p"); !errors.Is(err, ErrNotFound) {
		t.Errorf("404: %v", err)
	}
	status, answer = 409, `{"message":["Another open merge request already exists for this source branch: !4 `+testToken+`"]}`
	_, err := c.CreateMR(context.Background(), "g/p", NewMR{Source: "a", Target: "main", Title: "x"})
	if err == nil || !strings.Contains(err.Error(), "Another open merge request already exists") {
		t.Fatalf("409: %v", err)
	}
	if strings.Contains(err.Error(), testToken) {
		t.Error("the token is in the error")
	}
	if _, err := c.CreateMR(context.Background(), "g/p", NewMR{Source: "", Target: "main", Title: "x"}); err == nil {
		t.Error("no source branch accepted")
	}
}
