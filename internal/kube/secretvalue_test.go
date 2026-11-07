package kube_test

import (
	"context"
	"strings"
	"testing"
)

// CTX-40: one Secret value is read on demand, decoded from base64.
func TestSecretValueReadsOneKey(t *testing.T) {
	f, c := fakeClient(t)
	f.Set(t, "secret.txt", "aHVudGVyMg==") // "hunter2"
	v, err := c.SecretValue(context.Background(), "db-password", "password")
	if err != nil || v != "hunter2" {
		t.Fatalf("value = %q, %v", v, err)
	}
	got := f.Calls()
	if len(got) != 1 || !strings.HasPrefix(got[0], "get secret db-password -o go-template=") ||
		!strings.Contains(got[0], `index .data "password"`) || !strings.HasSuffix(got[0], "--context dev --namespace shop") {
		t.Errorf("calls = %v", got)
	}
}

func TestSecretValueMissingKey(t *testing.T) {
	f, c := fakeClient(t)
	f.Set(t, "secret.txt", "<no value>")
	if _, err := c.SecretValue(context.Background(), "db-password", "nope"); err == nil {
		t.Fatal("no error for a missing key")
	}
}

func TestSecretValueRefusesBadNames(t *testing.T) {
	f, c := fakeClient(t)
	for _, bad := range [][2]string{{"", "k"}, {"s", ""}, {"-A", "k"}, {"s", `k"}}{{.data}}`}, {"s", "a b"}} {
		if _, err := c.SecretValue(context.Background(), bad[0], bad[1]); err == nil {
			t.Errorf("SecretValue(%q, %q) did not fail", bad[0], bad[1])
		}
	}
	if n := len(f.Calls()); n != 0 {
		t.Errorf("kubectl ran %d times: %v", n, f.Calls())
	}
}
