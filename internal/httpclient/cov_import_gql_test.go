package httpclient

import (
	"strings"
	"testing"
)

// Postman exports GraphQL variables as a JSON text ("{\"id\":2}"). The
// request body needs them as a JSON object, not as a string of JSON;
// text that is not JSON is left out instead of sent as wrong variables.
func TestImportCollectionGraphQLVariablesGiven_AsJSONText(t *testing.T) {
	got := importOK(t, `{"item":[
	  {"name":"text","request":{"method":"POST","url":"https://x/g","body":{"mode":"graphql","graphql":{"query":"{a}","variables":"{\"id\":2}"}}}},
	  {"name":"bad","request":{"method":"POST","url":"https://x/g","body":{"mode":"graphql","graphql":{"query":"{a}","variables":"nonsense"}}}}
	]}`)
	byName := map[string]*Request{}
	for i := range got.Requests {
		byName[got.Requests[i].Name] = &got.Requests[i]
	}
	if b := bodyOf(byName["text"]); !strings.Contains(b, `"variables":{"id":2}`) {
		t.Errorf("variables as JSON text must become an object: %s", b)
	}
	if b := bodyOf(byName["bad"]); strings.Contains(b, "variables") {
		t.Errorf("variables that are not JSON are left out: %s", b)
	}
}
