package selector

import "testing"

func TestPullRequestURLKeepsRepository(t *testing.T) {
	got, err := PullRequest("https://bitbucket.org/other/project/pull-requests/42")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != 42 || got.Repository == nil || got.Repository.Workspace != "other" || got.Repository.Repo != "project" {
		t.Fatalf("unexpected selector: %+v", got)
	}
}

func TestPipelineBuildNumberAndURL(t *testing.T) {
	got, err := Pipeline("https://bitbucket.org/other/project/addon/pipelines/home#!/results/12")
	if err != nil {
		t.Fatal(err)
	}
	// URL fragments are not part of Path; the selector accepts the URL path
	// form used by the API and identifies the repository context.
	if got.Repository == nil || got.Repository.Workspace != "other" || got.Repository.Repo != "project" {
		t.Fatalf("unexpected repository: %+v", got.Repository)
	}

	got, err = Pipeline("12")
	if err != nil || got.BuildNumber == nil || *got.BuildNumber != 12 {
		t.Fatalf("unexpected build selector: %+v, %v", got, err)
	}
}
