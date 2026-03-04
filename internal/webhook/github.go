package webhook

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"text/template"

	"github.com/katalabut/openclaw-relay/internal/config"
	"github.com/katalabut/openclaw-relay/internal/gateway"
	"github.com/katalabut/openclaw-relay/internal/ratelimit"
)

type GitHubHandler struct {
	Config  *config.Config
	Gateway gateway.GatewayClient
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
	prTitle := payload.PullRequest.Title
	if prNumber == 0 && len(payload.CheckRun.PullRequests) > 0 {
		prNumber = payload.CheckRun.PullRequests[0].Number
	}
	if prNumber == 0 && len(payload.WorkflowRun.PullRequests) > 0 {
		prNumber = payload.WorkflowRun.PullRequests[0].Number
	}

	conclusion := payload.CheckRun.Conclusion
	if conclusion == "" {
		conclusion = payload.WorkflowRun.Conclusion
	}

	// notify_mode filtering: "failures" skips successful CI runs
	if h.Config.GitHub.NotifyMode == "failures" && conclusion == "success" {
		log.Printf("GitHub: skipping successful %s PR#%d (notify_mode=failures)", ghEvent, prNumber)
		w.WriteHeader(http.StatusOK)
		return
	}

	key := fmt.Sprintf("github:%s:%d", ghEvent, prNumber)
	if !h.Limiter.Allow(key) {
		log.Printf("GitHub: rate limited %s PR#%d", ghEvent, prNumber)
		w.WriteHeader(http.StatusOK)
		return
	}

	log.Printf("GitHub: processing %s/%s for %s PR#%d", ghEvent, payload.Action, payload.Repository.FullName, prNumber)

	// Render message from template
	tmplStr := h.Config.GitHub.MessageTemplate
	if tmplStr == "" {
		tmplStr = config.DefaultGitHubMessageTemplate()
	}

	data := map[string]interface{}{
		"Event":      ghEvent,
		"Action":     payload.Action,
		"Repository": payload.Repository.FullName,
		"PRNumber":   prNumber,
		"PRTitle":    prTitle,
		"Conclusion": conclusion,
	}

	msg := renderGitHubMessage(tmplStr, data)
	eventName := fmt.Sprintf("github %s/%s PR#%d", ghEvent, payload.Action, prNumber)

	timeout := h.Config.GitHub.Timeout
	if timeout == 0 {
		timeout = 120
	}
	delay := h.Config.GitHub.Delay
	if delay == 0 {
		delay = 2
	}

	agentID := h.Config.GitHub.AgentID
	if agentID != "" {
		if err := h.Gateway.CreateOneShotJobForAgent(eventName, msg, agentID, timeout, delay); err != nil {
			log.Printf("Failed to create job: %v", err)
		}
	} else {
		if err := h.Gateway.CreateOneShotJob(eventName, msg, timeout, delay); err != nil {
			log.Printf("Failed to create job: %v", err)
		}
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"ok":true}`))
}

func renderGitHubMessage(tmplStr string, data map[string]interface{}) string {
	tmpl, err := template.New("github").Parse(tmplStr)
	if err != nil {
		log.Printf("GitHub message template parse error: %v", err)
		return tmplStr
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		log.Printf("GitHub message template exec error: %v", err)
		return tmplStr
	}
	return buf.String()
}
