package gmail

import (
	"encoding/base64"
	"testing"

	gm "google.golang.org/api/gmail/v1"
)

func TestGetHeader_Found(t *testing.T) {
	headers := []*gm.MessagePartHeader{
		{Name: "Subject", Value: "Hello"},
		{Name: "From", Value: "test@example.com"},
	}
	if v := getHeader(headers, "subject"); v != "Hello" {
		t.Errorf("expected Hello, got %s", v)
	}
}

func TestGetHeader_NotFound(t *testing.T) {
	headers := []*gm.MessagePartHeader{
		{Name: "Subject", Value: "Hello"},
	}
	if v := getHeader(headers, "To"); v != "" {
		t.Errorf("expected empty, got %s", v)
	}
}

func TestExtractBody_PlainText(t *testing.T) {
	encoded := base64.URLEncoding.EncodeToString([]byte("Hello World"))
	payload := &gm.MessagePart{
		MimeType: "text/plain",
		Body:     &gm.MessagePartBody{Data: encoded},
	}
	body := extractBody(payload)
	if body != "Hello World" {
		t.Errorf("expected 'Hello World', got '%s'", body)
	}
}

func TestExtractBody_HTML(t *testing.T) {
	encoded := base64.URLEncoding.EncodeToString([]byte("<b>Hi</b>"))
	payload := &gm.MessagePart{
		MimeType: "text/html",
		Body:     &gm.MessagePartBody{Data: encoded},
	}
	body := extractBody(payload)
	if body != "<b>Hi</b>" {
		t.Errorf("expected '<b>Hi</b>', got '%s'", body)
	}
}

func TestExtractBody_Multipart(t *testing.T) {
	encoded := base64.URLEncoding.EncodeToString([]byte("Plain text"))
	payload := &gm.MessagePart{
		MimeType: "multipart/alternative",
		Parts: []*gm.MessagePart{
			{MimeType: "text/plain", Body: &gm.MessagePartBody{Data: encoded}},
		},
	}
	body := extractBody(payload)
	if body != "Plain text" {
		t.Errorf("expected 'Plain text', got '%s'", body)
	}
}

func TestExtractBody_Nil(t *testing.T) {
	if body := extractBody(nil); body != "" {
		t.Errorf("expected empty, got '%s'", body)
	}
}

func TestDecodeRFC2047_PlainString(t *testing.T) {
	result := decodeRFC2047("Hello World")
	if result != "Hello World" {
		t.Errorf("expected 'Hello World', got '%s'", result)
	}
}

func TestExtractBody_MultipartHTML(t *testing.T) {
	encoded := base64.URLEncoding.EncodeToString([]byte("<p>HTML</p>"))
	payload := &gm.MessagePart{
		MimeType: "multipart/alternative",
		Parts: []*gm.MessagePart{
			{MimeType: "text/html", Body: &gm.MessagePartBody{Data: encoded}},
		},
	}
	body := extractBody(payload)
	if body != "<p>HTML</p>" {
		t.Errorf("expected '<p>HTML</p>', got '%s'", body)
	}
}

func TestExtractBody_NestedMultipart(t *testing.T) {
	encoded := base64.URLEncoding.EncodeToString([]byte("nested"))
	payload := &gm.MessagePart{
		MimeType: "multipart/mixed",
		Parts: []*gm.MessagePart{
			{
				MimeType: "multipart/alternative",
				Parts: []*gm.MessagePart{
					{MimeType: "text/plain", Body: &gm.MessagePartBody{Data: encoded}},
				},
			},
		},
	}
	body := extractBody(payload)
	if body != "nested" {
		t.Errorf("expected 'nested', got '%s'", body)
	}
}

func TestExtractBody_EmptyBody(t *testing.T) {
	payload := &gm.MessagePart{
		MimeType: "text/plain",
		Body:     &gm.MessagePartBody{Data: ""},
	}
	body := extractBody(payload)
	if body != "" {
		t.Errorf("expected empty, got '%s'", body)
	}
}

func TestExtractBody_HTMLFallbackInMultipart(t *testing.T) {
	encoded := base64.URLEncoding.EncodeToString([]byte("<p>only html</p>"))
	payload := &gm.MessagePart{
		MimeType: "multipart/alternative",
		Parts: []*gm.MessagePart{
			{MimeType: "image/png", Body: &gm.MessagePartBody{}},
			{MimeType: "text/html", Body: &gm.MessagePartBody{Data: encoded}},
		},
	}
	body := extractBody(payload)
	if body != "<p>only html</p>" {
		t.Errorf("expected '<p>only html</p>', got '%s'", body)
	}
}

func TestDecodeRFC2047_Malformed(t *testing.T) {
	// Malformed RFC 2047 — should return original string
	result := decodeRFC2047("=?UTF-8?X?bad?=")
	if result != "=?UTF-8?X?bad?=" {
		t.Errorf("expected original string, got '%s'", result)
	}
}

func TestDecodeRFC2047_EncodedSubject(t *testing.T) {
	// RFC 2047 encoded string
	encoded := "=?UTF-8?B?0J/RgNC40LLQtdGC?="
	result := decodeRFC2047(encoded)
	if result != "Привет" {
		t.Errorf("expected 'Привет', got '%s'", result)
	}
}
