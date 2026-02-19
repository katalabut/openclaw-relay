package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/katalabut/openclaw-relay/internal/config"
	"github.com/katalabut/openclaw-relay/internal/gateway"
	"github.com/katalabut/openclaw-relay/internal/ratelimit"
)

type GitHubHandler struct {
	Config  *config.Config
	Gateway *gateway.Client
	Limiter *ratelimit.Limiter
}

func VerifyGitHubSignature(body []byte, signature, secret string) bool {
	if secret == "" {
		return true
	}
	if !strings.HasPrefix(signature, "sha256=") {
		return false
	}
	sig := strings.TrimPrefix(signature, "sha256=")
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(sig), []byte(expected))
}

func (h *GitHubHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	sig := r.Header.Get("X-Hub-Signature-256")
	if h.Config.GitHub.Secret != "" && !VerifyGitHubSignature(body, sig, h.Config.GitHub.Secret) {
		log.Printf("GitHub signature verification failed")
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	ghEvent := r.Header.Get("X-GitHub-Event")

	relevantEvents := map[string]bool{
		"check_run":           true,
		"workflow_run":        true,
		"pull_request_review": true,
	}
	if !relevantEvents[ghEvent] {
		log.Printf("GitHub: ignoring event %s", ghEvent)
		w.WriteHeader(http.StatusOK)
		return
	}

	var payload struct {
		Action     string `json:"action"`
		Repository struct {
			FullName string `json:"full_name"`
		} `json:"repository"`
		PullRequest struct {
			Number int    `json:"number"`
			Title  string `json:"title"`
		} `json:"pull_request"`
		CheckRun struct {
			Conclusion   string `json:"conclusion"`
			PullRequests []struct {
				Number int `json:"number"`
			} `json:"pull_requests"`
		} `json:"check_run"`
		WorkflowRun struct {
			Conclusion   string `json:"conclusion"`
			PullRequests []struct {
				Number int `json:"number"`
			} `json:"pull_requests"`
		} `json:"workflow_run"`
	}
	json.Unmarshal(body, &payload)

	switch ghEvent {
	case "check_run":
		if payload.Action != "completed" {
			w.WriteHeader(http.StatusOK)
			return
		}
	case "workflow_run":
		if payload.Action != "completed" {
			w.WriteHeader(http.StatusOK)
			return
		}
	case "pull_request_review":
		if payload.Action != "submitted" {
			w.WriteHeader(http.StatusOK)
			return
		}
	}

	prNumber := payload.PullRequest.Number
	if prNumber == 0 && len(payload.CheckRun.PullRequests) > 0 {
		prNumber = payload.CheckRun.PullRequests[0].Number
	}
	if prNumber == 0 && len(payload.WorkflowRun.PullRequests) > 0 {
		prNumber = payload.WorkflowRun.PullRequests[0].Number
	}

	key := fmt.Sprintf("github:%s:%d", ghEvent, prNumber)
	if !h.Limiter.Allow(key) {
		log.Printf("GitHub: rate limited %s PR#%d", ghEvent, prNumber)
		w.WriteHeader(http.StatusOK)
		return
	}

	log.Printf("GitHub: processing %s/%s for %s PR#%d", ghEvent, payload.Action, payload.Repository.FullName, prNumber)

	eventName := fmt.Sprintf("github %s/%s PR#%d", ghEvent, payload.Action, prNumber)
	msg := fmt.Sprintf(`[Webhook Event] GitHub event detected.

Source: github
Event: %s
Action: %s
Repository: %s
PR: #%d

Read skills/trello-tasks/SKILL.md.
Load board config from memory/trello-config.json.
Check if any card with label 'AI Review' (or in In Progress) has a PR matching this event.
If CI completed (success/failure) — check state.json for the card, act accordingly.
If PR review submitted — process review comments.
Telegram notifications: target=46075872, channel=telegram.
If nothing actionable, exit silently.`, ghEvent, payload.Action, payload.Repository.FullName, prNumber)

	if err := h.Gateway.CreateOneShotJob(eventName, msg, 120, 2); err != nil {
		log.Printf("Failed to create job: %v", err)
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"ok":true}`))
}
