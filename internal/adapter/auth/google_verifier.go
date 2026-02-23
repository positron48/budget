package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	useauth "github.com/positron48/budget/internal/usecase/auth"
)

type GoogleVerifier struct {
	clientID   string
	httpClient *http.Client
}

func NewGoogleVerifier(clientID string) *GoogleVerifier {
	return &GoogleVerifier{
		clientID: clientID,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

type googleTokenInfo struct {
	Audience      string `json:"aud"`
	Email         string `json:"email"`
	Name          string `json:"name"`
	Locale        string `json:"locale"`
	EmailVerified string `json:"email_verified"`
}

func (v *GoogleVerifier) VerifyIDToken(ctx context.Context, idToken string) (useauth.GoogleClaims, error) {
	endpoint := "https://oauth2.googleapis.com/tokeninfo?id_token=" + url.QueryEscape(strings.TrimSpace(idToken))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return useauth.GoogleClaims{}, err
	}

	resp, err := v.httpClient.Do(req)
	if err != nil {
		return useauth.GoogleClaims{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return useauth.GoogleClaims{}, fmt.Errorf("invalid google id token")
	}

	var info googleTokenInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return useauth.GoogleClaims{}, err
	}

	if info.Audience != v.clientID {
		return useauth.GoogleClaims{}, fmt.Errorf("google token audience mismatch")
	}
	if info.Email == "" {
		return useauth.GoogleClaims{}, fmt.Errorf("email is missing in google token")
	}

	return useauth.GoogleClaims{
		Email:         strings.ToLower(strings.TrimSpace(info.Email)),
		Name:          strings.TrimSpace(info.Name),
		Locale:        strings.TrimSpace(info.Locale),
		EmailVerified: strings.EqualFold(info.EmailVerified, "true"),
	}, nil
}
