package gateway

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

// GatewayClient is the interface for gateway operations.
type GatewayClient interface {
	// CreateOneShotJob creates a one-shot cron job for the default agent.
	// Use CreateOneShotJobForAgent to target a specific agent.
	CreateOneShotJob(name, message string, timeoutSeconds, delaySeconds int) error
	// CreateOneShotJobForAgent creates a one-shot cron job targeting a specific agent.
	// If agentID is empty, falls back to the client's default agent.
	CreateOneShotJobForAgent(name, message, agentID string, timeoutSeconds, delaySeconds int) error
}

type Client struct {
	URL     string
	Token   string
	AgentID string
	Model   string
	HTTP    *http.Client
}

func NewClient(url, token, agentID, model string) *Client {
	return &Client{
		URL:     strings.TrimRight(url, "/"),
		Token:   token,
		AgentID: agentID,
		Model:   model,
		HTTP:    &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *Client) CreateOneShotJob(name, message string, timeoutSeconds, delaySeconds int) error {
	return c.CreateOneShotJobForAgent(name, message, "", timeoutSeconds, delaySeconds)
}

func (c *Client) CreateOneShotJobForAgent(name, message, agentID string, timeoutSeconds, delaySeconds int) error {
	if c.URL == "" || c.Token == "" {
		log.Printf("Gateway not configured, skipping job creation for: %s", name)
		return nil
	}

	if agentID == "" {
		agentID = c.AgentID
	}

	fireAt := time.Now().Add(time.Duration(delaySeconds) * time.Second)
	job := map[string]interface{}{
		"name":          fmt.Sprintf("webhook: %s", name),
		"sessionTarget": "isolated",
		"enabled":       true,
		"schedule": map[string]interface{}{
			"kind": "at",
			"at":   fireAt.UTC().Format(time.RFC3339),
		},
		"payload": map[string]interface{}{
			"kind":           "agentTurn",
			"message":        message,
			"timeoutSeconds": timeoutSeconds,
		},
		"delivery": map[string]interface{}{
			"mode": "none",
		},
	}
	if c.Model != "" {
		job["payload"].(map[string]interface{})["model"] = c.Model
	}
	// Only set agentId if explicitly provided; gateway uses its default otherwise
	if agentID != "" {
		job["agentId"] = agentID
	}

	payload := map[string]interface{}{
		"action": "add",
		"job":    job,
	}

	body, _ := json.Marshal(payload)

	reqBody := map[string]interface{}{
		"tool":       "cron",
		"args":       json.RawMessage(body),
		"sessionKey": fmt.Sprintf("agent:%s:main", agentID),
	}
	reqJSON, _ := json.Marshal(reqBody)

	var lastErr error
	backoffs := []time.Duration{1 * time.Second, 2 * time.Second, 4 * time.Second}

	for attempt := 0; attempt <= len(backoffs); attempt++ {
		if attempt > 0 {
			log.Printf("Gateway retry %d/%d for: %s", attempt, len(backoffs), name)
			time.Sleep(backoffs[attempt-1])
		}

		lastErr = c.doRequest(reqJSON, agentID, name)
		if lastErr == nil {
			return nil
		}

		// Don't retry on 4xx errors
		if isClientError(lastErr) {
			return lastErr
		}
	}

	return fmt.Errorf("gateway request failed after %d attempts: %w", len(backoffs)+1, lastErr)
}

func (c *Client) doRequest(reqJSON []byte, agentID, name string) error {
	req, err := http.NewRequest("POST", c.URL+"/tools/invoke", bytes.NewReader(reqJSON))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.Token)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return &networkError{err: err}
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 && resp.StatusCode < 500 {
		return &clientError{status: resp.StatusCode, body: string(respBody)}
	}
	if resp.StatusCode >= 500 {
		return &serverError{status: resp.StatusCode, body: string(respBody)}
	}

	log.Printf("One-shot job created for agent=%s: %s", agentID, name)
	return nil
}

type networkError struct {
	err error
}

func (e *networkError) Error() string {
	return fmt.Sprintf("gateway network error: %v", e.err)
}

func (e *networkError) Unwrap() error { return e.err }

type clientError struct {
	status int
	body   string
}

func (e *clientError) Error() string {
	return fmt.Sprintf("gateway returned %d: %s", e.status, e.body)
}

type serverError struct {
	status int
	body   string
}

func (e *serverError) Error() string {
	return fmt.Sprintf("gateway returned %d: %s", e.status, e.body)
}

func isClientError(err error) bool {
	_, ok := err.(*clientError)
	return ok
}
