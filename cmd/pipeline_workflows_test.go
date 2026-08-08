package cmd

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestPipelineRunBranchPayload(t *testing.T) {
	var method, path string
	var body map[string]any
	out, err := run(t, func(r *http.Request) (*http.Response, error) {
		method, path = r.Method, r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		return jsonResp(201, `{"uuid":"run-1","state":{"name":"PENDING"}}`), nil
	}, "pipeline", "run", "--branch", "feature/test", "--custom", "deploy", "--variable", "ENV=staging")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if method != http.MethodPost || !strings.HasSuffix(path, "/pipelines/") {
		t.Fatalf("unexpected request %s %s", method, path)
	}
	if body["selector"].(map[string]any)["pattern"] != "deploy" {
		t.Fatalf("missing custom selector: %#v", body)
	}
	target := body["target"].(map[string]any)
	if target["ref_type"] != "branch" || target["ref_name"] != "feature/test" {
		t.Fatalf("unexpected target: %#v", target)
	}
	vars := body["variables"].([]any)[0].(map[string]any)
	if vars["key"] != "ENV" || vars["value"] != "staging" {
		t.Fatalf("unexpected variable: %#v", vars)
	}
	if !strings.Contains(out, `"uuid": "run-1"`) {
		t.Fatalf("unexpected output: %s", out)
	}
}

func TestPipelineRunRequiresOneTarget(t *testing.T) {
	called := false
	_, err := run(t, func(*http.Request) (*http.Response, error) { called = true; return nil, nil }, "pipeline", "run")
	if err == nil || !strings.Contains(err.Error(), "exactly one") {
		t.Fatalf("expected target validation error, got %v", err)
	}
	if called {
		t.Fatal("request should not be made")
	}
}

func TestPipelineStopRequiresConfirmation(t *testing.T) {
	called := false
	_, err := run(t, func(*http.Request) (*http.Response, error) { called = true; return jsonResp(200, `{}`), nil }, "pipeline", "stop", "run-1")
	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("expected confirmation error, got %v", err)
	}
	if called {
		t.Fatal("request should not be made")
	}
}

func TestPipelineVariableListRedactsSecuredValue(t *testing.T) {
	out, err := run(t, func(r *http.Request) (*http.Response, error) {
		return jsonResp(200, `{"values":[{"uuid":"v1","key":"TOKEN","value":"secret","secured":true},{"uuid":"v2","key":"PUBLIC","value":"visible","secured":false}]}`), nil
	}, "pipeline", "variable", "list")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(out, "secret") {
		t.Fatalf("secured value leaked: %s", out)
	}
	if !strings.Contains(out, "visible") {
		t.Fatalf("non-secured value missing: %s", out)
	}
}

func TestPipelineGetWarningIsStructured(t *testing.T) {
	out, err := run(t, func(r *http.Request) (*http.Response, error) {
		if strings.Contains(r.URL.Path, "/steps") {
			return jsonResp(500, `{"error":{"message":"unavailable"}}`), nil
		}
		return jsonResp(200, `{"uuid":"run-1","state":{"name":"COMPLETED"}}`), nil
	}, "pipeline", "get", "run-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatal(err)
	}
	warnings, ok := result["warnings"].([]any)
	if !ok || len(warnings) != 1 {
		t.Fatalf("expected one structured warning: %#v", result)
	}
	if _, ok := warnings[0].(map[string]any)["error"]; !ok {
		t.Fatalf("warning missing error: %#v", warnings[0])
	}
}
