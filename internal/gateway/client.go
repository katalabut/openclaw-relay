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

type Client struct {
	URL     string
	Token   string
	AgentID string
	HTTP    *http.Client
}

func NewClient(url, token, agentID string) *Client {
	return &Client{
		URL:     strings.TrimRight(url, "/"),
		Token:   token,
		AgentID: agentID,
		HTTP:    &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *Client) CreateOneShotJob(name, message string, timeoutSeconds, delaySeconds int) error {
	if c.URL == "" || c.Token == "" {
		log.Printf("Gateway not configured, skipping job creation for: %s", name)
		return nil
	}

	fireAt := time.Now().Add(time.Duration(delaySeconds) * time.Second)
	payload := map[string]interface{}{
		"action": "add",
		"job": map[string]interface{}{
			"name":          fmt.Sprintf("webhook: %s", name),
			"agentId":       c.AgentID,
			"sessionTarget": "isolated",
			"enabled":       true,
			"schedule": map[string]interface{}{
				"kind": "at",
				"at":   fireAt.UTC().Format(time.RFC3339),
			},
			"payload": map[string]interface{}{
				"kind":           "agentTurn",
				"message":        message,
				"model":          "anthropic/claude-sonnet-4-6",
				"timeoutSeconds": timeoutSeconds,
			},
			"delivery": map[string]interface{}{
				"mode": "none",
			},
		},
	}

	body, _ := json.Marshal(payload)

	reqBody := map[string]interface{}{
		"tool":       "cron",
		"args":       json.RawMessage(body),
		"sessionKey": fmt.Sprintf("agent:%s:main", c.AgentID),
	}
	reqJSON, _ := json.Marshal(reqBody)

	req, err := http.NewRequest("POST", c.URL+"/tools/invoke", bytes.NewReader(reqJSON))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.Token)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("gateway request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return fmt.Errorf("gateway returned %d: %s", resp.StatusCode, string(respBody))
	}

	log.Printf("One-shot job created: %s", name)
	return nil
}
