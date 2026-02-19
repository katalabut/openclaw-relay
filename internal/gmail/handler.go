package gmail

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

// Handler registers Gmail API HTTP handlers.
type Handler struct {
	client *Client
}

func NewHandler(client *Client) *Handler {
	return &Handler{client: client}
}

// RegisterRoutes adds Gmail API routes to the mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/gmail/messages", h.handleListMessages)
	mux.HandleFunc("/api/gmail/message/", h.handleGetMessage)
	mux.HandleFunc("/api/gmail/modify/", h.handleModifyMessage)
	mux.HandleFunc("/api/gmail/labels", h.handleListLabels)
	mux.HandleFunc("/api/gmail/threads/", h.handleGetThread)
}

func jsonResponse(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func jsonError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func (h *Handler) handleListMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	q := r.URL.Query().Get("q")
	if q == "" {
		q = "is:unread"
	}
	maxStr := r.URL.Query().Get("max")
	max := int64(20)
	if maxStr != "" {
		if v, err := strconv.ParseInt(maxStr, 10, 64); err == nil && v > 0 {
			max = v
		}
	}
	msgs, err := h.client.ListMessages(r.Context(), q, max)
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	jsonResponse(w, map[string]any{"messages": msgs})
}

func (h *Handler) handleGetMessage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/gmail/message/")
	if id == "" {
		jsonError(w, "missing message id", http.StatusBadRequest)
		return
	}
	msg, err := h.client.GetMessage(r.Context(), id)
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	jsonResponse(w, msg)
}

func (h *Handler) handleModifyMessage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/gmail/modify/")
	if id == "" {
		jsonError(w, "missing message id", http.StatusBadRequest)
		return
	}
	var req ModifyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if err := h.client.ModifyMessage(r.Context(), id, req); err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	jsonResponse(w, map[string]bool{"ok": true})
}

func (h *Handler) handleListLabels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	labels, err := h.client.ListLabels(r.Context())
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	jsonResponse(w, map[string]any{"labels": labels})
}

func (h *Handler) handleGetThread(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	threadID := strings.TrimPrefix(r.URL.Path, "/api/gmail/threads/")
	if threadID == "" {
		jsonError(w, "missing thread id", http.StatusBadRequest)
		return
	}
	msgs, err := h.client.GetThread(r.Context(), threadID)
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	jsonResponse(w, map[string]any{"messages": msgs})
}
