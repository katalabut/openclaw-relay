package tokens

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/oauth2"
)

// GoogleToken holds OAuth2 token data plus the authenticated email.
type GoogleToken struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	TokenType    string    `json:"token_type"`
	Expiry       time.Time `json:"expiry"`
	Email        string    `json:"email"`
}

// TokenData is the top-level structure persisted to disk.
type TokenData struct {
	Google *GoogleToken `json:"google,omitempty"`
}

// Store provides encrypted token persistence.
type Store struct {
	mu       sync.RWMutex
	filePath string
	key      []byte
	data     TokenData
}

// NewStore creates a token store. encKeyHex is a 32-byte hex-encoded AES key.
func NewStore(filePath, encKeyHex string) (*Store, error) {
	key, err := hex.DecodeString(encKeyHex)
	if err != nil || len(key) != 32 {
		return nil, fmt.Errorf("RELAY_ENCRYPTION_KEY must be 32-byte hex (64 chars)")
	}
	s := &Store{filePath: filePath, key: key}
	if err := s.load(); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("load tokens: %w", err)
	}
	return s, nil
}

func (s *Store) encrypt(plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

func (s *Store) decrypt(ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	ns := gcm.NonceSize()
	if len(ciphertext) < ns {
		return nil, fmt.Errorf("ciphertext too short")
	}
	return gcm.Open(nil, ciphertext[:ns], ciphertext[ns:], nil)
}

func (s *Store) load() error {
	data, err := os.ReadFile(s.filePath)
	if err != nil {
		return err
	}
	plaintext, err := s.decrypt(data)
	if err != nil {
		return fmt.Errorf("decrypt: %w", err)
	}
	return json.Unmarshal(plaintext, &s.data)
}

func (s *Store) save() error {
	if err := os.MkdirAll(filepath.Dir(s.filePath), 0700); err != nil {
		return err
	}
	plaintext, err := json.Marshal(s.data)
	if err != nil {
		return err
	}
	encrypted, err := s.encrypt(plaintext)
	if err != nil {
		return err
	}
	return os.WriteFile(s.filePath, encrypted, 0600)
}

// SaveGoogle stores a Google OAuth token.
func (s *Store) SaveGoogle(token *oauth2.Token, email string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.Google = &GoogleToken{
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		TokenType:    token.TokenType,
		Expiry:       token.Expiry,
		Email:        email,
	}
	return s.save()
}

// GetGoogle returns the stored Google token, or nil.
func (s *Store) GetGoogle() *GoogleToken {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.data.Google
}

// GetGoogleOAuth2Token converts to oauth2.Token.
func (s *Store) GetGoogleOAuth2Token() *oauth2.Token {
	g := s.GetGoogle()
	if g == nil {
		return nil
	}
	return &oauth2.Token{
		AccessToken:  g.AccessToken,
		RefreshToken: g.RefreshToken,
		TokenType:    g.TokenType,
		Expiry:       g.Expiry,
	}
}

// UpdateGoogleAccessToken updates just the access token and expiry (after refresh).
func (s *Store) UpdateGoogleAccessToken(token *oauth2.Token) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data.Google == nil {
		return fmt.Errorf("no google token to update")
	}
	s.data.Google.AccessToken = token.AccessToken
	s.data.Google.Expiry = token.Expiry
	if token.RefreshToken != "" {
		s.data.Google.RefreshToken = token.RefreshToken
	}
	return s.save()
}

// ClearGoogle removes stored Google tokens.
func (s *Store) ClearGoogle() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.Google = nil
	return s.save()
}
