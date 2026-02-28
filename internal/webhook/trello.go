package webhook

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
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

type TrelloHandler struct {
	Config  *config.Config
	Gateway gateway.GatewayClient
	Limiter *ratelimit.Limiter
}

type trelloPayload struct {
	Action struct {
		Type string `json:"type"`
		Data struct {
			Card struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"card"`
			ListAfter struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"listAfter"`
			ListBefore struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"listBefore"`
		} `json:"data"`
	} `json:"action"`
}

func VerifyTrelloSignature(body []byte, signature, secret, callbackURL string) bool {
	if secret == "" {
		return true
	}
	mac := hmac.New(sha1.New, []byte(secret))
	mac.Write(body)
	mac.Write([]byte(callbackURL))
	expected := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(signature), []byte(expected)) {
		log.Printf("Trello sig mismatch: got=%s expected=%s callbackURL=%s", signature, expected, callbackURL)
		return false
	}
	return true
}

func (h *TrelloHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	sig := r.Header.Get("X-Trello-Webhook")
	callbackURL := "https://" + r.Host + r.URL.Path
	if h.Config.Trello.Secret != "" && !VerifyTrelloSignature(body, sig, h.Config.Trello.Secret, callbackURL) {
		log.Printf("Trello signature verification failed")
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	var payload trelloPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		log.Printf("Failed to parse Trello payload: %v", err)
		w.WriteHeader(http.StatusOK)
		return
	}

	actionType := payload.Action.Type
	cardID := payload.Action.Data.Card.ID
	cardName := payload.Action.Data.Card.Name
	listAfterID := payload.Action.Data.ListAfter.ID
	listAfterName := payload.Action.Data.ListAfter.Name
	listBeforeName := payload.Action.Data.ListBefore.Name

	var eventType string
	switch actionType {
	case "updateCard":
		if listAfterID == "" {
			log.Printf("Trello: ignoring updateCard without list change for %s", cardName)
			w.WriteHeader(http.StatusOK)
			return
		}
		listName := h.Config.ListIDToName(listAfterID)
		if listName == "" {
			log.Printf("Trello: ignoring move to unwatched list %s for %s", listAfterName, cardName)
			w.WriteHeader(http.StatusOK)
			return
		}
		// Skip card moves TO Questions — comment-only column
		if listName == "questions" {
			log.Printf("Trello: ignoring move to Questions for %s (comment-only column)", cardName)
			w.WriteHeader(http.StatusOK)
			return
		}
		eventType = "card_moved"
	case "commentCard":
		if cardID == "" {
			log.Printf("Trello: ignoring comment without card ID")
			w.WriteHeader(http.StatusOK)
			return
		}
		eventType = "comment_added"
	default:
		log.Printf("Trello: ignoring action %s", actionType)
		w.WriteHeader(http.StatusOK)
		return
	}

	// Rate limit
	rateLimitKey := fmt.Sprintf("trello:%s:%s", cardID, actionType)
	if !h.Limiter.Allow(rateLimitKey) {
		log.Printf("Trello: rate limited card %s (%s) action %s", cardName, cardID, actionType)
		w.WriteHeader(http.StatusOK)
		return
	}

	log.Printf("Trello: processing %s for card %s", eventType, cardName)

	// Find matching rule
	listName := h.Config.ListIDToName(listAfterID)
	rule := h.findRule(eventType, listName)
	if rule == nil {
		log.Printf("Trello: no matching rule for event=%s list=%s", eventType, listName)
		w.WriteHeader(http.StatusOK)
		return
	}

	// Render message
	msg := h.renderMessage(rule.Action.MessageTemplate, map[string]string{
		"CardID":         cardID,
		"CardName":       cardName,
		"ListAfterID":    listAfterID,
		"ListAfterName":  listAfterName,
		"ListBeforeName": listBeforeName,
		"ListName":       listAfterName,
	})

	timeout := rule.Action.Timeout
	if timeout == 0 {
		timeout = 120
	}
	delay := rule.Action.Delay
	if delay == 0 {
		delay = 2
	}

	eventName := fmt.Sprintf("%s: %s", eventType, cardName)
	if err := h.Gateway.CreateOneShotJobForAgent(eventName, msg, rule.Action.AgentID, timeout, delay); err != nil {
		log.Printf("Failed to create job: %v", err)
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"ok":true}`))
}

func (h *TrelloHandler) findRule(eventType, listName string) *config.TrelloRule {
	for i, rule := range h.Config.Trello.Rules {
		if rule.Event != eventType {
			continue
		}
		if h.matchCondition(rule.Condition, listName) {
			return &h.Config.Trello.Rules[i]
		}
	}
	return nil
}

func (h *TrelloHandler) matchCondition(condition, listName string) bool {
	if condition == "" {
		return true
	}
	// Simple condition parser: "list == 'ready'" or "list == 'x' || list == 'y'"
	parts := strings.Split(condition, "||")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if strings.Contains(part, "list ==") {
			// Extract quoted value
			start := strings.Index(part, "'")
			end := strings.LastIndex(part, "'")
			if start >= 0 && end > start {
				val := part[start+1 : end]
				if val == listName {
					return true
				}
			}
		}
	}
	return false
}

func (h *TrelloHandler) renderMessage(tmpl string, data map[string]string) string {
	t, err := template.New("msg").Parse(tmpl)
	if err != nil {
		log.Printf("Template parse error: %v", err)
		return tmpl
	}
	var buf strings.Builder
	if err := t.Execute(&buf, data); err != nil {
		log.Printf("Template exec error: %v", err)
		return tmpl
	}
	return buf.String()
}
