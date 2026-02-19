package webhook

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/katalabut/openclaw-relay/internal/config"
	"github.com/katalabut/openclaw-relay/internal/ratelimit"
)

type mockGateway struct {
	calls []mockGatewayCall
}

type mockGatewayCall struct {
	Name    string
	Message string
	Timeout int
	Delay   int
}

func (m *mockGateway) CreateOneShotJob(name, message string, timeoutSeconds, delaySeconds int) error {
	m.calls = append(m.calls, mockGatewayCall{name, message, timeoutSeconds, delaySeconds})
	return nil
}

func TestVerifyTrelloSignature(t *testing.T) {
	if !VerifyTrelloSignature([]byte("body"), "sig", "", "url") {
		t.Error("empty secret should pass")
	}
}

func TestVerifyTrelloSignature_Valid(t *testing.T) {
	body := []byte("hello")
	secret := "mysecret"
	callbackURL := "https://example.com/webhook/trello"
	mac := hmac.New(sha1.New, []byte(secret))
	mac.Write(body)
	mac.Write([]byte(callbackURL))
	sig := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	if !VerifyTrelloSignature(body, sig, secret, callbackURL) {
		t.Error("valid signature should pass")
	}
}

func TestVerifyTrelloSignature_Invalid(t *testing.T) {
	if VerifyTrelloSignature([]byte("body"), "badsig", "secret", "url") {
		t.Error("invalid signature should fail")
	}
}

func TestMatchCondition(t *testing.T) {
	h := &TrelloHandler{}
	tests := []struct {
		cond string
		list string
		want bool
	}{
		{"list == 'ready'", "ready", true},
		{"list == 'ready'", "dev", false},
		{"list == 'in_progress' || list == 'dev' || list == 'prod'", "dev", true},
		{"list == 'in_progress' || list == 'dev' || list == 'prod'", "ready", false},
		{"", "anything", true},
	}
	for _, tt := range tests {
		got := h.matchCondition(tt.cond, tt.list)
		if got != tt.want {
			t.Errorf("matchCondition(%q, %q) = %v, want %v", tt.cond, tt.list, got, tt.want)
		}
	}
}

func newTestTrelloHandler(gw *mockGateway) *TrelloHandler {
	cfg := &config.Config{
		Trello: config.TrelloConfig{
			Secret: "",
			Lists: map[string]string{
				"ready":     "list-ready-id",
				"questions": "list-questions-id",
			},
			Rules: []config.TrelloRule{
				{
					Event:     "card_moved",
					Condition: "list == 'ready'",
					Action: config.RuleAction{
						Kind:            "one_shot",
						Timeout:         120,
						Delay:           2,
						MessageTemplate: "Card {{.CardName}} moved to {{.ListAfterName}}",
					},
				},
				{
					Event:     "comment_added",
					Condition: "list == 'questions'",
					Action: config.RuleAction{
						Kind:            "one_shot",
						Timeout:         180,
						Delay:           180,
						MessageTemplate: "Comment on {{.CardName}}",
					},
				},
			},
		},
	}
	return &TrelloHandler{
		Config:  cfg,
		Gateway: gw,
		Limiter: ratelimit.New(5 * time.Minute),
	}
}

func makeTrelloPayload(actionType, cardID, cardName, listAfterID, listAfterName, listBeforeID, listBeforeName string) []byte {
	p := map[string]interface{}{
		"action": map[string]interface{}{
			"type": actionType,
			"data": map[string]interface{}{
				"card":       map[string]string{"id": cardID, "name": cardName},
				"listAfter":  map[string]string{"id": listAfterID, "name": listAfterName},
				"listBefore": map[string]string{"id": listBeforeID, "name": listBeforeName},
			},
		},
	}
	b, _ := json.Marshal(p)
	return b
}

func TestServeHTTP_InvalidSignature(t *testing.T) {
	gw := &mockGateway{}
	h := newTestTrelloHandler(gw)
	h.Config.Trello.Secret = "secret"

	body := []byte(`{"action":{"type":"updateCard"}}`)
	req := httptest.NewRequest("POST", "/webhook/trello", bytes.NewReader(body))
	req.Header.Set("X-Trello-Webhook", "invalidsig")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rec.Code)
	}
}

func TestServeHTTP_CardMoved_WatchedList(t *testing.T) {
	gw := &mockGateway{}
	h := newTestTrelloHandler(gw)

	body := makeTrelloPayload("updateCard", "card1", "My Card", "list-ready-id", "Ready", "", "Dev")
	req := httptest.NewRequest("POST", "/webhook/trello", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	if len(gw.calls) != 1 {
		t.Fatalf("expected 1 gateway call, got %d", len(gw.calls))
	}
	if gw.calls[0].Timeout != 120 {
		t.Errorf("expected timeout 120, got %d", gw.calls[0].Timeout)
	}
}

func TestServeHTTP_CardMoved_UnwatchedList(t *testing.T) {
	gw := &mockGateway{}
	h := newTestTrelloHandler(gw)

	body := makeTrelloPayload("updateCard", "card1", "My Card", "unknown-list-id", "Unknown", "", "Dev")
	req := httptest.NewRequest("POST", "/webhook/trello", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	if len(gw.calls) != 0 {
		t.Error("expected no gateway calls for unwatched list")
	}
}

func TestServeHTTP_CardMoved_QuestionsColumn(t *testing.T) {
	gw := &mockGateway{}
	h := newTestTrelloHandler(gw)

	body := makeTrelloPayload("updateCard", "card1", "My Card", "list-questions-id", "Questions", "", "Dev")
	req := httptest.NewRequest("POST", "/webhook/trello", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	if len(gw.calls) != 0 {
		t.Error("expected no gateway calls for questions column move")
	}
}

func TestServeHTTP_Comment_QuestionsColumn(t *testing.T) {
	gw := &mockGateway{}
	h := newTestTrelloHandler(gw)

	// commentCard with card in questions list - need listAfter to be questions
	p := map[string]interface{}{
		"action": map[string]interface{}{
			"type": "commentCard",
			"data": map[string]interface{}{
				"card":      map[string]string{"id": "card1", "name": "My Card"},
				"listAfter": map[string]string{"id": "list-questions-id", "name": "Questions"},
			},
		},
	}
	body, _ := json.Marshal(p)
	req := httptest.NewRequest("POST", "/webhook/trello", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	if len(gw.calls) != 1 {
		t.Fatalf("expected 1 gateway call, got %d", len(gw.calls))
	}
	if gw.calls[0].Delay != 180 {
		t.Errorf("expected delay 180, got %d", gw.calls[0].Delay)
	}
}

func TestServeHTTP_Comment_OtherColumn(t *testing.T) {
	gw := &mockGateway{}
	h := newTestTrelloHandler(gw)

	p := map[string]interface{}{
		"action": map[string]interface{}{
			"type": "commentCard",
			"data": map[string]interface{}{
				"card":      map[string]string{"id": "card2", "name": "Card2"},
				"listAfter": map[string]string{"id": "unknown-list", "name": "Other"},
			},
		},
	}
	body, _ := json.Marshal(p)
	req := httptest.NewRequest("POST", "/webhook/trello", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if len(gw.calls) != 0 {
		t.Error("expected no gateway calls for comment on other column")
	}
}

func TestFindRule_MatchFirst(t *testing.T) {
	h := newTestTrelloHandler(&mockGateway{})
	rule := h.findRule("card_moved", "ready")
	if rule == nil {
		t.Fatal("expected to find rule")
	}
	if rule.Action.Timeout != 120 {
		t.Errorf("wrong rule matched")
	}
}

func TestFindRule_NoMatch(t *testing.T) {
	h := newTestTrelloHandler(&mockGateway{})
	rule := h.findRule("card_moved", "nonexistent")
	if rule != nil {
		t.Error("expected no match")
	}
}

func TestRenderMessage_AllVars(t *testing.T) {
	h := &TrelloHandler{}
	msg := h.renderMessage("Card {{.CardName}} to {{.ListAfterName}}", map[string]string{
		"CardName":      "Test Card",
		"ListAfterName": "Ready",
	})
	if msg != "Card Test Card to Ready" {
		t.Errorf("unexpected: %s", msg)
	}
}

func TestRenderMessage_InvalidTemplate(t *testing.T) {
	h := &TrelloHandler{}
	tmpl := "{{.Invalid"
	msg := h.renderMessage(tmpl, map[string]string{})
	if msg != tmpl {
		t.Errorf("expected raw template on error, got: %s", msg)
	}
}

func TestServeHTTP_RateLimited(t *testing.T) {
	gw := &mockGateway{}
	h := newTestTrelloHandler(gw)

	body := makeTrelloPayload("updateCard", "card1", "My Card", "list-ready-id", "Ready", "", "Dev")

	// First request
	req := httptest.NewRequest("POST", "/webhook/trello", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if len(gw.calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(gw.calls))
	}

	// Second request - should be rate limited
	req = httptest.NewRequest("POST", "/webhook/trello", bytes.NewReader(body))
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if len(gw.calls) != 1 {
		t.Errorf("expected still 1 call (rate limited), got %d", len(gw.calls))
	}
}

func TestServeHTTP_MethodNotAllowed(t *testing.T) {
	gw := &mockGateway{}
	h := newTestTrelloHandler(gw)

	req := httptest.NewRequest("GET", "/webhook/trello", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rec.Code)
	}
}

func TestServeHTTP_InvalidJSON(t *testing.T) {
	gw := &mockGateway{}
	h := newTestTrelloHandler(gw)

	req := httptest.NewRequest("POST", "/webhook/trello", bytes.NewReader([]byte("not json")))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestServeHTTP_IgnoredAction(t *testing.T) {
	gw := &mockGateway{}
	h := newTestTrelloHandler(gw)

	p := map[string]interface{}{
		"action": map[string]interface{}{
			"type": "addMemberToBoard",
			"data": map[string]interface{}{},
		},
	}
	body, _ := json.Marshal(p)
	req := httptest.NewRequest("POST", "/webhook/trello", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if len(gw.calls) != 0 {
		t.Error("expected no calls for ignored action")
	}
}

func TestServeHTTP_UpdateCardNoListChange(t *testing.T) {
	gw := &mockGateway{}
	h := newTestTrelloHandler(gw)

	// updateCard without listAfter (e.g., description change)
	p := map[string]interface{}{
		"action": map[string]interface{}{
			"type": "updateCard",
			"data": map[string]interface{}{
				"card": map[string]string{"id": "c1", "name": "Card"},
			},
		},
	}
	body, _ := json.Marshal(p)
	req := httptest.NewRequest("POST", "/webhook/trello", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if len(gw.calls) != 0 {
		t.Error("expected no calls for updateCard without list change")
	}
}

func TestServeHTTP_CommentNoCardID(t *testing.T) {
	gw := &mockGateway{}
	h := newTestTrelloHandler(gw)

	p := map[string]interface{}{
		"action": map[string]interface{}{
			"type": "commentCard",
			"data": map[string]interface{}{
				"card": map[string]string{"id": "", "name": ""},
			},
		},
	}
	body, _ := json.Marshal(p)
	req := httptest.NewRequest("POST", "/webhook/trello", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if len(gw.calls) != 0 {
		t.Error("expected no calls for comment without card ID")
	}
}

func TestRenderMessage_ExecuteError(t *testing.T) {
	h := &TrelloHandler{}
	// Template that calls a method on a string (will fail on execute)
	tmpl := "{{.CardName.Bad}}"
	msg := h.renderMessage(tmpl, map[string]string{"CardName": "test"})
	if msg != tmpl {
		t.Errorf("expected raw template on execute error, got: %s", msg)
	}
}

func TestServeHTTP_HeadRequest(t *testing.T) {
	gw := &mockGateway{}
	h := newTestTrelloHandler(gw)

	req := httptest.NewRequest("HEAD", "/webhook/trello", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("HEAD should return 200, got %d", rec.Code)
	}
}
