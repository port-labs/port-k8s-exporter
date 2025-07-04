package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/port-labs/port-k8s-exporter/pkg/config"
	"golang.org/x/oauth2"
	"io"
	"net/http"
	"strings"
	"time"
)

type portTokenSource struct {
	ClientID     string
	ClientSecret string
	Endpoint     string
	HTTPClient   *http.Client
}

type accessTokenResponse struct {
	AccessToken string `json:"accessToken"`
	ExpiresIn   int    `json:"expiresIn"`
}

func newTokenSource(cfg *config.ApplicationConfiguration) oauth2.TokenSource {
	return &portTokenSource{
		ClientID:     cfg.PortClientId,
		ClientSecret: cfg.PortClientSecret,
		Endpoint:     cfg.PortBaseURL,
		HTTPClient:   http.DefaultClient,
	}
}

func (ts *portTokenSource) Token() (*oauth2.Token, error) {
	// Create a timeout context for the token request
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	reqBody := strings.NewReader(fmt.Sprintf(`{"clientId":"%s","clientSecret":"%s"}`, ts.ClientID, ts.ClientSecret))
	req, err := http.NewRequestWithContext(ctx, "POST", fmt.Sprintf("%s/v1/auth/access_token", ts.Endpoint), reqBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := ts.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			return
		}
	}(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("port auth failed: status %d", resp.StatusCode)
	}

	var tokenResp accessTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("failed to decode access token: %w", err)
	}

	return &oauth2.Token{
		AccessToken: tokenResp.AccessToken,
		TokenType:   "Bearer",
		Expiry:      time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second),
	}, nil
}
