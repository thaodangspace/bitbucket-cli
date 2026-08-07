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

func TestCommentAndTaskAPIFields(t *testing.T) {
	comment, err := CommentFields.Project(map[string]any{"resolution": map[string]any{"type": "resolved"}}, []string{"resolution.type"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(comment, map[string]any{"resolution": map[string]any{"type": "resolved"}}) {
		t.Fatalf("unexpected comment projection: %#v", comment)
	}
	task, err := TaskFields.Project(map[string]any{"creator": map[string]any{"display_name": "A"}, "pending": true}, []string{"creator.display_name", "pending"})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"creator": map[string]any{"display_name": "A"}, "pending": true}
	if !reflect.DeepEqual(task, want) {
		t.Fatalf("unexpected task projection: %#v", task)
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

func TestInsightsUseBitbucketTimestampFields(t *testing.T) {
	value := map[string]any{"created_on": "2025-01-01T00:00:00Z", "updated_on": "2025-01-02T00:00:00Z"}
	got, err := ReportFields.Project(value, []string{"created_on", "updated_on"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, value) {
		t.Fatalf("unexpected report projection: %#v", got)
	}
	if _, err := AnnotationFields.Project(value, []string{"created_at"}); err == nil {
		t.Fatal("expected created_at to be rejected")
	}
}
