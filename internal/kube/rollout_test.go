package kube_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/amin-jalali/termocode/internal/kube"
)

func TestRolloutStatusIsReadOnlyAndDoesNotWait(t *testing.T) {
	f, c := fakeClient(t)
	f.Set(t, "rollout-status.txt", "Waiting for deployment \"api\" rollout to finish: 1 of 3 updated replicas are available...\n")
	got, err := c.RolloutStatus(context.Background(), "api")
	if err != nil || got != "Waiting for deployment \"api\" rollout to finish: 1 of 3 updated replicas are available..." {
		t.Fatalf("status = %q, %v", got, err)
	}
	if calls := f.Calls(); len(calls) != 1 || calls[0] != "rollout status deployment/api --watch=false --context dev --namespace shop" {
		t.Errorf("calls = %v", calls)
	}
}

func TestRolloutHistoryParsesRevisions(t *testing.T) {
	f, c := fakeClient(t)
	f.Set(t, "rollout-history.txt", "deployment.apps/api \nREVISION  CHANGE-CAUSE\n1         <none>\n2         kubectl set image deployment/api api=shop/api:2\n3         <none>\n\n")
	got, err := c.RolloutHistory(context.Background(), "api")
	if err != nil {
		t.Fatal(err)
	}
	want := []kube.Revision{{Number: 1}, {Number: 2, Cause: "kubectl set image deployment/api api=shop/api:2"}, {Number: 3}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("history = %+v", got)
	}
	if calls := f.Calls(); calls[0] != "rollout history deployment/api --context dev --namespace shop" {
		t.Errorf("calls = %v", calls)
	}
}

func TestRolloutUndo(t *testing.T) {
	f, c := fakeClient(t)
	if err := c.RolloutUndo(context.Background(), "api", 0); err != nil {
		t.Fatal(err)
	}
	if err := c.RolloutUndo(context.Background(), "api", 2); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"rollout undo deployment/api --context dev --namespace shop",
		"rollout undo deployment/api --to-revision=2 --context dev --namespace shop",
	}
	if got := f.Calls(); !reflect.DeepEqual(got, want) {
		t.Errorf("calls = %v", got)
	}
	f.Set(t, "undo.err", "error: no rollout history found for deployment \"api\"\n")
	if err := c.RolloutUndo(context.Background(), "api", 0); err == nil {
		t.Error("want the kubectl error")
	}
}

func TestSetImage(t *testing.T) {
	f, c := fakeClient(t)
	if err := c.SetImage(context.Background(), "api", "api", "shop/api:3"); err != nil {
		t.Fatal(err)
	}
	if got := f.Calls(); len(got) != 1 || got[0] != "set image deployment/api api=shop/api:3 --context dev --namespace shop" {
		t.Errorf("calls = %v", got)
	}
}

func TestRolloutRefusesFlagLikeInput(t *testing.T) {
	f, c := fakeClient(t)
	ctx := context.Background()
	if err := c.SetImage(ctx, "api", "api", "--kubeconfig=/x"); err == nil {
		t.Error("image starting with - must be refused")
	}
	if err := c.SetImage(ctx, "api", "a=b", "img"); err == nil {
		t.Error("container with = must be refused")
	}
	if err := c.SetImage(ctx, "api", "api", ""); err == nil {
		t.Error("empty image must be refused")
	}
	if err := c.RolloutUndo(ctx, "-x", 0); err == nil {
		t.Error("name starting with - must be refused")
	}
	if err := c.RolloutUndo(ctx, "api", -1); err == nil {
		t.Error("negative revision must be refused")
	}
	if _, err := c.RolloutStatus(ctx, ""); err == nil {
		t.Error("empty name must be refused")
	}
	if n := len(f.Calls()); n != 0 {
		t.Errorf("refused input still ran kubectl %d times", n)
	}
}
