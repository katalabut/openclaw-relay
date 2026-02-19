package webhook

import (
	"testing"
)

func TestVerifyTrelloSignature(t *testing.T) {
	// Empty secret should pass
	if !VerifyTrelloSignature([]byte("body"), "sig", "", "url") {
		t.Error("empty secret should pass")
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
