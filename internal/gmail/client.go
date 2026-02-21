package gmail

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"mime"
	"strings"

	"github.com/katalabut/openclaw-relay/internal/tokens"
	"golang.org/x/oauth2"
	gm "google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"
)

// GmailClient is the interface for Gmail operations.
type GmailClient interface {
	ListMessages(ctx context.Context, query string, maxResults int64) ([]MessageMeta, error)
	GetMessage(ctx context.Context, id string) (*MessageFull, error)
	ModifyMessage(ctx context.Context, id string, req ModifyRequest) error
	ListLabels(ctx context.Context) ([]LabelInfo, error)
	GetThread(ctx context.Context, threadID string) ([]MessageFull, error)
	GetCurrentHistoryID(ctx context.Context) (uint64, error)
	GetHistory(ctx context.Context, startHistoryID uint64) ([]HistoryMessage, uint64, error)
}

// Client wraps Gmail API v1.
type Client struct {
	store    *tokens.Store
	oauthCfg *oauth2.Config
	email    string
}

func NewClient(store *tokens.Store, oauthCfg *oauth2.Config) *Client {
	return &Client{store: store, oauthCfg: oauthCfg}
}

func NewClientForAccount(store *tokens.Store, oauthCfg *oauth2.Config, email string) *Client {
	return &Client{store: store, oauthCfg: oauthCfg, email: email}
}

func (c *Client) getService(ctx context.Context) (*gm.Service, error) {
	tok := c.store.GetGoogleOAuth2Token(c.email)
	if tok == nil {
		if c.email == "" {
			return nil, fmt.Errorf("not authenticated with Google")
		}
		return nil, fmt.Errorf("not authenticated with Google for %s", c.email)
	}
	ts := c.oauthCfg.TokenSource(ctx, tok)
	// Get a fresh token (auto-refreshes if expired)
	newTok, err := ts.Token()
	if err != nil {
		return nil, fmt.Errorf("token refresh: %w", err)
	}
	// Persist refreshed token
	if newTok.AccessToken != tok.AccessToken {
		if err := c.store.UpdateGoogleAccessToken(newTok, c.email); err != nil {
			log.Printf("Warning: failed to persist refreshed token: %v", err)
		}
	}
	return gm.NewService(ctx, option.WithTokenSource(ts))
}

// MessageMeta is a lightweight message representation.
type MessageMeta struct {
	ID       string   `json:"id"`
	ThreadID string   `json:"threadId"`
	Subject  string   `json:"subject"`
	From     string   `json:"from"`
	Date     string   `json:"date"`
	Snippet  string   `json:"snippet"`
	Labels   []string `json:"labels"`
}

// MessageFull is a full message representation.
type MessageFull struct {
	ID       string   `json:"id"`
	ThreadID string   `json:"threadId"`
	Subject  string   `json:"subject"`
	From     string   `json:"from"`
	To       string   `json:"to"`
	Date     string   `json:"date"`
	Body     string   `json:"body"`
	Labels   []string `json:"labels"`
	Snippet  string   `json:"snippet"`
}

func getHeader(headers []*gm.MessagePartHeader, name string) string {
	for _, h := range headers {
		if strings.EqualFold(h.Name, name) {
			return h.Value
		}
	}
	return ""
}

func extractBody(payload *gm.MessagePart) string {
	if payload == nil {
		return ""
	}
	// Try plain text first
	if payload.MimeType == "text/plain" && payload.Body != nil && payload.Body.Data != "" {
		data, _ := base64.URLEncoding.DecodeString(payload.Body.Data)
		return string(data)
	}
	// Multipart
	for _, part := range payload.Parts {
		if part.MimeType == "text/plain" && part.Body != nil && part.Body.Data != "" {
			data, _ := base64.URLEncoding.DecodeString(part.Body.Data)
			return string(data)
		}
	}
	// Fallback to HTML
	if payload.MimeType == "text/html" && payload.Body != nil && payload.Body.Data != "" {
		data, _ := base64.URLEncoding.DecodeString(payload.Body.Data)
		return string(data)
	}
	for _, part := range payload.Parts {
		if part.MimeType == "text/html" && part.Body != nil && part.Body.Data != "" {
			data, _ := base64.URLEncoding.DecodeString(part.Body.Data)
			return string(data)
		}
		// Nested multipart
		body := extractBody(part)
		if body != "" {
			return body
		}
	}
	return ""
}

func decodeRFC2047(s string) string {
	dec := new(mime.WordDecoder)
	result, err := dec.DecodeHeader(s)
	if err != nil {
		return s
	}
	return result
}

// ListMessages lists messages matching a query.
func (c *Client) ListMessages(ctx context.Context, query string, maxResults int64) ([]MessageMeta, error) {
	svc, err := c.getService(ctx)
	if err != nil {
		return nil, err
	}
	if maxResults <= 0 {
		maxResults = 20
	}
	call := svc.Users.Messages.List("me").Q(query).MaxResults(maxResults)
	resp, err := call.Do()
	if err != nil {
		return nil, fmt.Errorf("list messages: %w", err)
	}

	var msgs []MessageMeta
	for _, m := range resp.Messages {
		msg, err := svc.Users.Messages.Get("me", m.Id).Format("metadata").MetadataHeaders("Subject", "From", "Date").Do()
		if err != nil {
			log.Printf("Warning: get message %s: %v", m.Id, err)
			continue
		}
		msgs = append(msgs, MessageMeta{
			ID:       msg.Id,
			ThreadID: msg.ThreadId,
			Subject:  decodeRFC2047(getHeader(msg.Payload.Headers, "Subject")),
			From:     decodeRFC2047(getHeader(msg.Payload.Headers, "From")),
			Date:     getHeader(msg.Payload.Headers, "Date"),
			Snippet:  msg.Snippet,
			Labels:   msg.LabelIds,
		})
	}
	return msgs, nil
}

// GetMessage gets a full message by ID.
func (c *Client) GetMessage(ctx context.Context, id string) (*MessageFull, error) {
	svc, err := c.getService(ctx)
	if err != nil {
		return nil, err
	}
	msg, err := svc.Users.Messages.Get("me", id).Format("full").Do()
	if err != nil {
		return nil, fmt.Errorf("get message: %w", err)
	}
	return &MessageFull{
		ID:       msg.Id,
		ThreadID: msg.ThreadId,
		Subject:  decodeRFC2047(getHeader(msg.Payload.Headers, "Subject")),
		From:     decodeRFC2047(getHeader(msg.Payload.Headers, "From")),
		To:       decodeRFC2047(getHeader(msg.Payload.Headers, "To")),
		Date:     getHeader(msg.Payload.Headers, "Date"),
		Body:     extractBody(msg.Payload),
		Labels:   msg.LabelIds,
		Snippet:  msg.Snippet,
	}, nil
}

// ModifyRequest describes label modifications.
type ModifyRequest struct {
	AddLabels    []string `json:"addLabels"`
	RemoveLabels []string `json:"removeLabels"`
	Archive      bool     `json:"archive"`
	MarkRead     bool     `json:"markRead"`
	Star         bool     `json:"star"`
}

// ModifyMessage modifies labels on a message.
func (c *Client) ModifyMessage(ctx context.Context, id string, req ModifyRequest) error {
	svc, err := c.getService(ctx)
	if err != nil {
		return err
	}
	mod := &gm.ModifyMessageRequest{
		AddLabelIds:    req.AddLabels,
		RemoveLabelIds: req.RemoveLabels,
	}
	if req.Archive {
		mod.RemoveLabelIds = append(mod.RemoveLabelIds, "INBOX")
	}
	if req.MarkRead {
		mod.RemoveLabelIds = append(mod.RemoveLabelIds, "UNREAD")
	}
	if req.Star {
		mod.AddLabelIds = append(mod.AddLabelIds, "STARRED")
	}
	_, err = svc.Users.Messages.Modify("me", id, mod).Do()
	return err
}

// LabelInfo is a label.
type LabelInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

// ListLabels lists all labels.
func (c *Client) ListLabels(ctx context.Context) ([]LabelInfo, error) {
	svc, err := c.getService(ctx)
	if err != nil {
		return nil, err
	}
	resp, err := svc.Users.Labels.List("me").Do()
	if err != nil {
		return nil, err
	}
	var labels []LabelInfo
	for _, l := range resp.Labels {
		labels = append(labels, LabelInfo{ID: l.Id, Name: l.Name, Type: l.Type})
	}
	return labels, nil
}

// GetThread gets all messages in a thread.
func (c *Client) GetThread(ctx context.Context, threadID string) ([]MessageFull, error) {
	svc, err := c.getService(ctx)
	if err != nil {
		return nil, err
	}
	thread, err := svc.Users.Threads.Get("me", threadID).Format("full").Do()
	if err != nil {
		return nil, err
	}
	var msgs []MessageFull
	for _, msg := range thread.Messages {
		msgs = append(msgs, MessageFull{
			ID:       msg.Id,
			ThreadID: msg.ThreadId,
			Subject:  decodeRFC2047(getHeader(msg.Payload.Headers, "Subject")),
			From:     decodeRFC2047(getHeader(msg.Payload.Headers, "From")),
			To:       decodeRFC2047(getHeader(msg.Payload.Headers, "To")),
			Date:     getHeader(msg.Payload.Headers, "Date"),
			Body:     extractBody(msg.Payload),
			Labels:   msg.LabelIds,
			Snippet:  msg.Snippet,
		})
	}
	return msgs, nil
}

// GetCurrentHistoryID returns the latest historyId.
func (c *Client) GetCurrentHistoryID(ctx context.Context) (uint64, error) {
	svc, err := c.getService(ctx)
	if err != nil {
		return 0, err
	}
	profile, err := svc.Users.GetProfile("me").Do()
	if err != nil {
		return 0, err
	}
	return profile.HistoryId, nil
}

// HistoryMessage is a new message from history.
type HistoryMessage struct {
	ID       string   `json:"id"`
	ThreadID string   `json:"threadId"`
	Labels   []string `json:"labels"`
	Subject  string   `json:"subject"`
	From     string   `json:"from"`
	Snippet  string   `json:"snippet"`
}

// GetHistory returns new messages since startHistoryId.
func (c *Client) GetHistory(ctx context.Context, startHistoryID uint64) ([]HistoryMessage, uint64, error) {
	svc, err := c.getService(ctx)
	if err != nil {
		return nil, 0, err
	}

	var allMsgs []HistoryMessage
	var newHistoryID uint64
	pageToken := ""

	for {
		call := svc.Users.History.List("me").StartHistoryId(startHistoryID).HistoryTypes("messageAdded")
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}
		resp, err := call.Do()
		if err != nil {
			return nil, 0, fmt.Errorf("history.list: %w", err)
		}
		newHistoryID = resp.HistoryId

		for _, h := range resp.History {
			for _, ma := range h.MessagesAdded {
				msg := ma.Message
				// Get metadata for the message
				full, err := svc.Users.Messages.Get("me", msg.Id).Format("metadata").MetadataHeaders("Subject", "From").Do()
				if err != nil {
					log.Printf("Warning: get history message %s: %v", msg.Id, err)
					allMsgs = append(allMsgs, HistoryMessage{
						ID:       msg.Id,
						ThreadID: msg.ThreadId,
						Labels:   msg.LabelIds,
					})
					continue
				}
				allMsgs = append(allMsgs, HistoryMessage{
					ID:       full.Id,
					ThreadID: full.ThreadId,
					Labels:   full.LabelIds,
					Subject:  decodeRFC2047(getHeader(full.Payload.Headers, "Subject")),
					From:     decodeRFC2047(getHeader(full.Payload.Headers, "From")),
					Snippet:  full.Snippet,
				})
			}
		}

		if resp.NextPageToken == "" {
			break
		}
		pageToken = resp.NextPageToken
	}

	return allMsgs, newHistoryID, nil
}
