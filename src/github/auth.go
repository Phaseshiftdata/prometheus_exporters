package github

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/bradleyfalzon/ghinstallation/v2"
)

// Auth handles GitHub App authentication and installation token management.
// It is safe for concurrent use.
type Auth struct {
	appID         int64
	installID     int64
	privateKey    []byte
	transport     *ghinstallation.Transport
	mu            sync.Mutex
	cachedToken   string
	tokenExpiry   time.Time
	nowFunc       func() time.Time // for testing
	tokenRefresh  func(ctx context.Context) (string, time.Time, error)
}

// NewAuth creates a new Auth by reading the PEM private key from keyFile.
func NewAuth(appID, installID int64, keyFile string) (*Auth, error) {
	key, err := os.ReadFile(keyFile)
	if err != nil {
		return nil, fmt.Errorf("reading private key: %w", err)
	}
	return NewAuthFromKey(appID, installID, key)
}

// NewAuthFromKey creates a new Auth from raw PEM private key bytes.
func NewAuthFromKey(appID, installID int64, key []byte) (*Auth, error) {
	tr, err := ghinstallation.New(http.DefaultTransport, appID, installID, key)
	if err != nil {
		return nil, fmt.Errorf("creating installation transport: %w", err)
	}

	a := &Auth{
		appID:      appID,
		installID:  installID,
		privateKey: key,
		transport:  tr,
		nowFunc:    time.Now,
	}
	a.tokenRefresh = a.defaultTokenRefresh
	return a, nil
}

// Token returns a valid installation token, refreshing it if expired.
// The token has a 1-hour TTL; we refresh 5 minutes early to avoid races.
func (a *Auth) Token(ctx context.Context) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	now := a.nowFunc()
	// Refresh 5 minutes before actual expiry to avoid edge cases.
	if a.cachedToken != "" && now.Before(a.tokenExpiry.Add(-5*time.Minute)) {
		return a.cachedToken, nil
	}

	token, expiry, err := a.tokenRefresh(ctx)
	if err != nil {
		return "", fmt.Errorf("refreshing installation token: %w", err)
	}

	a.cachedToken = token
	a.tokenExpiry = expiry
	return a.cachedToken, nil
}

func (a *Auth) defaultTokenRefresh(ctx context.Context) (string, time.Time, error) {
	token, err := a.transport.Token(ctx)
	if err != nil {
		return "", time.Time{}, err
	}
	expiry := a.nowFunc().Add(1 * time.Hour)
	return token, expiry, nil
}
