package httpclient

import (
	"strings"
	"testing"
)

// The collection's auth reaches every request, also one written as a
// bare URL string.
func TestImportCollectionAuthReachesABareURLRequest(t *testing.T) {
	got := importOK(t, `{"auth":{"type":"bearer","bearer":[{"key":"token","value":"{{tok}}"}]},
	  "item":[{"name":"bare","request":"https://example.org/bare"}]}`)
	if v := got.Requests[0].Header("Authorization"); !strings.HasPrefix(v, "Bearer") {
		t.Errorf("Authorization = %q, want the collection's bearer token", v)
	}
}
