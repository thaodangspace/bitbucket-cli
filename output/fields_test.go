package output

import (
	"reflect"
	"testing"
)

func TestNestedProjectionDoesNotLeakArrayFields(t *testing.T) {
	value := map[string]any{
		"reviewers": []any{map[string]any{"display_name": "A", "account_id": "secret"}},
		"steps":     []any{map[string]any{"name": "build", "state": "hidden"}},
	}
	got, err := PullRequestFields.Project(value, []string{"reviewers.display_name"})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"reviewers": []any{map[string]any{"display_name": "A"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestProjectionRejectsScalarDescent(t *testing.T) {
	if _, err := PullRequestFields.Project(map[string]any{"title": "x"}, []string{"title.anything"}); err == nil {
		t.Fatal("expected scalar descent error")
	}
}

func TestProjectionOmitsMissingFields(t *testing.T) {
	got, err := PullRequestFields.Project(map[string]any{"id": 1}, []string{"title", "id"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, map[string]any{"id": 1}) {
		t.Fatalf("unexpected projection: %#v", got)
	}
}
