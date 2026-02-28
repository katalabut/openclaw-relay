package webhook

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/katalabut/openclaw-relay/internal/config"
	"github.com/katalabut/openclaw-relay/internal/ratelimit"
)

func TestVerifyGitHubSignature_Valid(t *testing.T) {
	body := []byte("payload")
	secret := "mysecret"
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	if !VerifyGitHubSignature(body, sig, secret) {
		t.Error("valid signature should pass")
	}
}

func TestVerifyGitHubSignature_Invalid(t *testing.T) {
	if VerifyGitHubSignature([]byte("body"), "sha256=bad", "secret") {
		t.Error("invalid signature should fail")
	}
}

func TestVerifyGitHubSignature_EmptySecret(t *testing.T) {
	if !VerifyGitHubSignature([]byte("body"), "", "") {
		t.Error("empty secret should pass")
	}
}

func TestVerifyGitHubSignature_NoPrefix(t *testing.T) {
	if VerifyGitHubSignature([]byte("body"), "noprefixhash", "secret") {
		t.Error("missing sha256= prefix should fail")
	}
}

func newTestGitHubHandler(gw *mockGateway) *GitHubHandler {
	cfg := &config.Config{
		GitHub: config.GitHubConfig{Secret: "", NotifyMode: "all"},
	}
	return &GitHubHandler{
		Config:  cfg,
		Gateway: gw,
		Limiter: ratelimit.New(5 * time.Minute),
	}
}

func TestServeHTTP_GitHub_InvalidSignature(t *testing.T) {
	gw := &mockGateway{}
	h := newTestGitHubHandler(gw)
	h.Config.GitHub.Secret = "secret"

	req := httptest.NewRequest("POST", "/webhook/github", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("X-Hub-Signature-256", "sha256=invalid")
	req.Header.Set("X-GitHub-Event", "push")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rec.Code)
	}
}

func TestServeHTTP_GitHub_PullRequestReview(t *testing.T) {
	gw := &mockGateway{}
	h := newTestGitHubHandler(gw)

	payload := map[string]interface{}{
		"action": "submitted",
		"repository": map[string]string{
			"full_name": "user/repo",
		},
		"pull_request": map[string]interface{}{
			"number": 42,
			"title":  "Fix bug",
		},
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest("POST", "/webhook/github", bytes.NewReader(body))
	req.Header.Set("X-GitHub-Event", "pull_request_review")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	if len(gw.calls) != 1 {
		t.Fatalf("expected 1 gateway call, got %d", len(gw.calls))
	}
}

func TestServeHTTP_GitHub_WorkflowRun(t *testing.T) {
	gw := &mockGateway{}
	h := newTestGitHubHandler(gw)

	payload := map[string]interface{}{
		"action": "completed",
		"repository": map[string]string{
			"full_name": "user/repo",
		},
		"workflow_run": map[string]interface{}{
			"conclusion": "success",
			"pull_requests": []map[string]interface{}{
				{"number": 10},
			},
		},
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest("POST", "/webhook/github", bytes.NewReader(body))
	req.Header.Set("X-GitHub-Event", "workflow_run")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	if len(gw.calls) != 1 {
		t.Fatalf("expected 1 gateway call, got %d", len(gw.calls))
	}
}

func TestServeHTTP_GitHub_IgnoredEvent(t *testing.T) {
	gw := &mockGateway{}
	h := newTestGitHubHandler(gw)

	req := httptest.NewRequest("POST", "/webhook/github", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("X-GitHub-Event", "push")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	if len(gw.calls) != 0 {
		t.Error("expected no gateway calls for ignored event")
	}
}

func TestServeHTTP_GitHub_CheckRunCompleted(t *testing.T) {
	gw := &mockGateway{}
	h := newTestGitHubHandler(gw)

	payload := map[string]interface{}{
		"action": "completed",
		"repository": map[string]string{"full_name": "user/repo"},
		"check_run": map[string]interface{}{
			"conclusion":    "success",
			"pull_requests": []map[string]interface{}{{"number": 5}},
		},
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest("POST", "/webhook/github", bytes.NewReader(body))
	req.Header.Set("X-GitHub-Event", "check_run")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if len(gw.calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(gw.calls))
	}
}

func TestServeHTTP_GitHub_CheckRunNotCompleted(t *testing.T) {
	gw := &mockGateway{}
	h := newTestGitHubHandler(gw)

	payload := map[string]interface{}{
		"action":     "created",
		"repository": map[string]string{"full_name": "user/repo"},
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest("POST", "/webhook/github", bytes.NewReader(body))
	req.Header.Set("X-GitHub-Event", "check_run")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if len(gw.calls) != 0 {
		t.Error("expected no calls for non-completed check_run")
	}
}

func TestServeHTTP_GitHub_WorkflowNotCompleted(t *testing.T) {
	gw := &mockGateway{}
	h := newTestGitHubHandler(gw)

	payload := map[string]interface{}{
		"action":     "requested",
		"repository": map[string]string{"full_name": "user/repo"},
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest("POST", "/webhook/github", bytes.NewReader(body))
	req.Header.Set("X-GitHub-Event", "workflow_run")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if len(gw.calls) != 0 {
		t.Error("expected no calls for non-completed workflow_run")
	}
}

func TestServeHTTP_GitHub_PRReviewNotSubmitted(t *testing.T) {
	gw := &mockGateway{}
	h := newTestGitHubHandler(gw)

	payload := map[string]interface{}{
		"action":     "dismissed",
		"repository": map[string]string{"full_name": "user/repo"},
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest("POST", "/webhook/github", bytes.NewReader(body))
	req.Header.Set("X-GitHub-Event", "pull_request_review")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if len(gw.calls) != 0 {
		t.Error("expected no calls for dismissed review")
	}
}

func TestServeHTTP_GitHub_RateLimited(t *testing.T) {
	gw := &mockGateway{}
	h := newTestGitHubHandler(gw)

	payload := map[string]interface{}{
		"action":     "submitted",
		"repository": map[string]string{"full_name": "user/repo"},
		"pull_request": map[string]interface{}{
			"number": 99,
			"title":  "Test",
		},
	}
	body, _ := json.Marshal(payload)

	// First request
	req := httptest.NewRequest("POST", "/webhook/github", bytes.NewReader(body))
	req.Header.Set("X-GitHub-Event", "pull_request_review")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	// Second - rate limited
	req = httptest.NewRequest("POST", "/webhook/github", bytes.NewReader(body))
	req.Header.Set("X-GitHub-Event", "pull_request_review")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if len(gw.calls) != 1 {
		t.Errorf("expected 1 call (rate limited), got %d", len(gw.calls))
	}
}

func TestServeHTTP_GitHub_MethodNotAllowed(t *testing.T) {
	gw := &mockGateway{}
	h := newTestGitHubHandler(gw)

	req := httptest.NewRequest("GET", "/webhook/github", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rec.Code)
	}
}

func TestServeHTTP_GitHub_NotifyFailures_SkipsSuccessWorkflow(t *testing.T) {
	gw := &mockGateway{}
	h := newTestGitHubHandler(gw)
	h.Config.GitHub.NotifyMode = "failures"

	payload := map[string]interface{}{
		"action": "completed",
		"repository": map[string]string{
			"full_name": "user/repo",
		},
		"workflow_run": map[string]interface{}{
			"conclusion": "success",
			"pull_requests": []map[string]interface{}{{"number": 11}},
		},
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest("POST", "/webhook/github", bytes.NewReader(body))
	req.Header.Set("X-GitHub-Event", "workflow_run")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if len(gw.calls) != 0 {
		t.Fatalf("expected 0 gateway calls for success in failures mode, got %d", len(gw.calls))
	}
}

func TestServeHTTP_GitHub_NotifyFailures_AllowsFailureWorkflow(t *testing.T) {
	gw := &mockGateway{}
	h := newTestGitHubHandler(gw)
	h.Config.GitHub.NotifyMode = "failures"

	payload := map[string]interface{}{
		"action": "completed",
		"repository": map[string]string{
			"full_name": "user/repo",
		},
		"workflow_run": map[string]interface{}{
			"conclusion": "failure",
			"pull_requests": []map[string]interface{}{{"number": 12}},
		},
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest("POST", "/webhook/github", bytes.NewReader(body))
	req.Header.Set("X-GitHub-Event", "workflow_run")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if len(gw.calls) != 1 {
		t.Fatalf("expected 1 gateway call for failure in failures mode, got %d", len(gw.calls))
	}
}
