package kube_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/kube"
)

// CTX-28: `auth can-i --list` is a table with bracket lists that hold
// spaces; it is read by the header's column starts.
const canITable = `Resources                                       Non-Resource URLs   Resource Names   Verbs
pods                                            []                  []               [get list watch]
deployments.apps                                []                  [web]            [get patch]
                                                [/healthz]          []               [get]
selfsubjectaccessreviews.authorization.k8s.io   []                  []               [create]
`

func TestParseCanIListReadsTheTable(t *testing.T) {
	got, err := kube.ParseCanIList(canITable)
	if err != nil {
		t.Fatal(err)
	}
	want := []kube.Permission{
		{Resource: "pods", Verbs: []string{"get", "list", "watch"}},
		{Resource: "deployments.apps", Names: []string{"web"}, Verbs: []string{"get", "patch"}},
		{URLs: []string{"/healthz"}, Verbs: []string{"get"}},
		{Resource: "selfsubjectaccessreviews.authorization.k8s.io", Verbs: []string{"create"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
	if !got[0].Allows("list") || got[0].Allows("delete") {
		t.Fatal("Allows")
	}
	if !(kube.Permission{Verbs: []string{"*"}}).Allows("delete") {
		t.Fatal("* allows every verb")
	}
}

func TestParseCanIListRejectsAnUnknownHeader(t *testing.T) {
	if _, err := kube.ParseCanIList("NAME  READY\nx  1/1\n"); err == nil {
		t.Fatal("a table without Resources/Verbs must fail")
	}
	if got, err := kube.ParseCanIList(""); err != nil || len(got) != 0 {
		t.Fatalf("empty answer = %v, %v", got, err)
	}
}

func TestCanIListPassesContextAndNamespaceAndIsReadOnly(t *testing.T) {
	f, c := fakeClient(t)
	f.Set(t, "can-i.txt", canITable)
	perms, err := c.CanIList(context.Background())
	if err != nil || len(perms) != 4 {
		t.Fatalf("perms = %d, %v", len(perms), err)
	}
	if got := f.Calls(); len(got) != 1 || got[0] != "auth can-i --list --context dev --namespace shop" {
		t.Fatalf("calls = %v", got)
	}
}

func TestCanIListForbiddenIsOneQuietLine(t *testing.T) {
	f, c := fakeClient(t)
	f.Set(t, "can-i.err", `Error from server (Forbidden): selfsubjectrulesreviews.authorization.k8s.io is forbidden: User "x" cannot create resource`+"\n")
	_, err := c.CanIList(context.Background())
	var ke *kube.Error
	if err == nil || !errors.As(err, &ke) || ke.Kind != kube.ErrForbidden {
		t.Fatalf("err = %v", err)
	}
}

func TestRoleBindingsListSubjectsAndRole(t *testing.T) {
	f, c := fakeClient(t)
	b, err := os.ReadFile(filepath.Join("testdata", "rolebindings.json"))
	if err != nil {
		t.Fatal(err)
	}
	f.Set(t, "rolebindings.json", string(b))
	got, err := c.RoleBindings(context.Background())
	if err != nil || len(got) != 2 {
		t.Fatalf("bindings = %+v, %v", got, err)
	}
	if got[0].Name != "shop-readers" || got[0].Role != "ClusterRole/view" ||
		strings.Join(got[0].Subjects, ",") != "Group/dev-team,ServiceAccount/build/ci" {
		t.Fatalf("first = %+v", got[0])
	}
	if got[1].Role != "Role/admin" || strings.Join(got[1].Subjects, ",") != "User/amy@example.com" {
		t.Fatalf("second = %+v", got[1])
	}
	if calls := f.Calls(); len(calls) != 1 || calls[0] != "get rolebindings -o json --context dev --namespace shop" {
		t.Fatalf("calls = %v", calls)
	}
}
