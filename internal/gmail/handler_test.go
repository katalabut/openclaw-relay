package gmail

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type mockGmailClient struct {
	listMessagesFunc  func(ctx context.Context, query string, max int64) ([]MessageMeta, error)
	getMessageFunc    func(ctx context.Context, id string) (*MessageFull, error)
	modifyMessageFunc func(ctx context.Context, id string, req ModifyRequest) error
	listLabelsFunc    func(ctx context.Context) ([]LabelInfo, error)
	getThreadFunc     func(ctx context.Context, id string) ([]MessageFull, error)
	getCurrentHIDFunc func(ctx context.Context) (uint64, error)
	getHistoryFunc    func(ctx context.Context, startHID uint64) ([]HistoryMessage, uint64, error)
}

func (m *mockGmailClient) ListMessages(ctx context.Context, query string, max int64) ([]MessageMeta, error) {
	return m.listMessagesFunc(ctx, query, max)
}
func (m *mockGmailClient) GetMessage(ctx context.Context, id string) (*MessageFull, error) {
	return m.getMessageFunc(ctx, id)
}
func (m *mockGmailClient) ModifyMessage(ctx context.Context, id string, req ModifyRequest) error {
	return m.modifyMessageFunc(ctx, id, req)
}
func (m *mockGmailClient) ListLabels(ctx context.Context) ([]LabelInfo, error) {
	return m.listLabelsFunc(ctx)
}
func (m *mockGmailClient) GetThread(ctx context.Context, id string) ([]MessageFull, error) {
	return m.getThreadFunc(ctx, id)
}
func (m *mockGmailClient) GetCurrentHistoryID(ctx context.Context) (uint64, error) {
	return m.getCurrentHIDFunc(ctx)
}
func (m *mockGmailClient) GetHistory(ctx context.Context, startHID uint64) ([]HistoryMessage, uint64, error) {
	return m.getHistoryFunc(ctx, startHID)
}

func TestHandleListMessages_OK(t *testing.T) {
	mc := &mockGmailClient{
		listMessagesFunc: func(_ context.Context, q string, max int64) ([]MessageMeta, error) {
			return []MessageMeta{{ID: "msg1", Subject: "Test"}}, nil
		},
	}
	h := NewHandler(mc)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/api/gmail/messages?q=is:unread", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp map[string][]MessageMeta
	json.NewDecoder(rec.Body).Decode(&resp)
	if len(resp["messages"]) != 1 {
		t.Errorf("expected 1 message, got %d", len(resp["messages"]))
	}
}

func TestHandleListMessages_ClientError(t *testing.T) {
	mc := &mockGmailClient{
		listMessagesFunc: func(_ context.Context, _ string, _ int64) ([]MessageMeta, error) {
			return nil, fmt.Errorf("auth error")
		},
	}
	h := NewHandler(mc)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/api/gmail/messages", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != 500 {
		t.Errorf("expected 500, got %d", rec.Code)
	}
}

func TestHandleGetMessage_OK(t *testing.T) {
	mc := &mockGmailClient{
		getMessageFunc: func(_ context.Context, id string) (*MessageFull, error) {
			return &MessageFull{ID: id, Subject: "Hello"}, nil
		},
	}
	h := NewHandler(mc)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/api/gmail/message/msg123", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var msg MessageFull
	json.NewDecoder(rec.Body).Decode(&msg)
	if msg.ID != "msg123" {
		t.Errorf("expected msg123, got %s", msg.ID)
	}
}

func TestHandleGetMessage_NotFound(t *testing.T) {
	mc := &mockGmailClient{
		getMessageFunc: func(_ context.Context, _ string) (*MessageFull, error) {
			return nil, fmt.Errorf("not found")
		},
	}
	h := NewHandler(mc)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/api/gmail/message/bad", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != 500 {
		t.Errorf("expected 500, got %d", rec.Code)
	}
}

func TestHandleModifyMessage_OK(t *testing.T) {
	mc := &mockGmailClient{
		modifyMessageFunc: func(_ context.Context, _ string, _ ModifyRequest) error {
			return nil
		},
	}
	h := NewHandler(mc)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	body := `{"addLabels":["STARRED"],"markRead":true}`
	req := httptest.NewRequest("POST", "/api/gmail/modify/msg123", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestHandleModifyMessage_BadBody(t *testing.T) {
	mc := &mockGmailClient{}
	h := NewHandler(mc)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest("POST", "/api/gmail/modify/msg123", strings.NewReader("not json"))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != 400 {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestHandleListLabels_OK(t *testing.T) {
	mc := &mockGmailClient{
		listLabelsFunc: func(_ context.Context) ([]LabelInfo, error) {
			return []LabelInfo{{ID: "l1", Name: "INBOX", Type: "system"}}, nil
		},
	}
	h := NewHandler(mc)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/api/gmail/labels", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestHandleListMessages_MethodNotAllowed(t *testing.T) {
	mc := &mockGmailClient{}
	h := NewHandler(mc)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest("POST", "/api/gmail/messages", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != 405 {
		t.Errorf("expected 405, got %d", rec.Code)
	}
}

func TestHandleGetMessage_MissingID(t *testing.T) {
	mc := &mockGmailClient{}
	h := NewHandler(mc)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/api/gmail/message/", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != 400 {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestHandleModifyMessage_MissingID(t *testing.T) {
	mc := &mockGmailClient{}
	h := NewHandler(mc)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest("POST", "/api/gmail/modify/", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != 400 {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestHandleModifyMessage_Error(t *testing.T) {
	mc := &mockGmailClient{
		modifyMessageFunc: func(_ context.Context, _ string, _ ModifyRequest) error {
			return fmt.Errorf("fail")
		},
	}
	h := NewHandler(mc)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest("POST", "/api/gmail/modify/msg1", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != 500 {
		t.Errorf("expected 500, got %d", rec.Code)
	}
}

func TestHandleListLabels_Error(t *testing.T) {
	mc := &mockGmailClient{
		listLabelsFunc: func(_ context.Context) ([]LabelInfo, error) {
			return nil, fmt.Errorf("fail")
		},
	}
	h := NewHandler(mc)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/api/gmail/labels", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != 500 {
		t.Errorf("expected 500, got %d", rec.Code)
	}
}

func TestHandleListLabels_MethodNotAllowed(t *testing.T) {
	mc := &mockGmailClient{}
	h := NewHandler(mc)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest("POST", "/api/gmail/labels", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != 405 {
		t.Errorf("expected 405, got %d", rec.Code)
	}
}

func TestHandleGetThread_Error(t *testing.T) {
	mc := &mockGmailClient{
		getThreadFunc: func(_ context.Context, _ string) ([]MessageFull, error) {
			return nil, fmt.Errorf("fail")
		},
	}
	h := NewHandler(mc)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/api/gmail/threads/t1", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != 500 {
		t.Errorf("expected 500, got %d", rec.Code)
	}
}

func TestHandleGetThread_MissingID(t *testing.T) {
	mc := &mockGmailClient{}
	h := NewHandler(mc)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/api/gmail/threads/", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != 400 {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestHandleGetMessage_MethodNotAllowed(t *testing.T) {
	mc := &mockGmailClient{}
	h := NewHandler(mc)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest("POST", "/api/gmail/message/msg1", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != 405 {
		t.Errorf("expected 405, got %d", rec.Code)
	}
}

func TestHandleGetThread_MethodNotAllowed(t *testing.T) {
	mc := &mockGmailClient{}
	h := NewHandler(mc)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest("POST", "/api/gmail/threads/t1", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != 405 {
		t.Errorf("expected 405, got %d", rec.Code)
	}
}

func TestHandleModifyMessage_MethodNotAllowed(t *testing.T) {
	mc := &mockGmailClient{}
	h := NewHandler(mc)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/api/gmail/modify/msg1", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != 405 {
		t.Errorf("expected 405, got %d", rec.Code)
	}
}

func TestHandleListMessages_CustomMaxResults(t *testing.T) {
	var gotMax int64
	mc := &mockGmailClient{
		listMessagesFunc: func(_ context.Context, _ string, max int64) ([]MessageMeta, error) {
			gotMax = max
			return nil, nil
		},
	}
	h := NewHandler(mc)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/api/gmail/messages?max=5", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if gotMax != 5 {
		t.Errorf("expected max=5, got %d", gotMax)
	}
}

func TestHandleGetThread_OK(t *testing.T) {
	mc := &mockGmailClient{
		getThreadFunc: func(_ context.Context, id string) ([]MessageFull, error) {
			return []MessageFull{{ID: "m1", ThreadID: id}}, nil
		},
	}
	h := NewHandler(mc)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/api/gmail/threads/t1", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}
