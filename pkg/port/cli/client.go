package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/go-resty/resty/v2"
	"github.com/port-labs/port-k8s-exporter/pkg/config"
	"github.com/port-labs/port-k8s-exporter/pkg/port"
)

type (
	Option     func(*PortClient)
	PortClient struct {
		Client                       *resty.Client
		ClientID                     string
		ClientSecret                 string
		DeleteDependents             bool
		CreateMissingRelatedEntities bool
	}
)

func New(applicationConfig *config.ApplicationConfiguration, opts ...Option) *PortClient {
	c := &PortClient{
		Client: resty.New().
			SetBaseURL(applicationConfig.PortBaseURL).
			SetRetryCount(5).
			SetRetryWaitTime(300).
			// retry when create permission fails because scopes are created async-ly and sometimes (mainly in tests) the scope doesn't exist yet.
			AddRetryCondition(func(r *resty.Response, err error) bool {
				if err != nil {
					return true
				}
				if !strings.Contains(r.Request.URL, "/permissions") {
					return false
				}
				b := make(map[string]interface{})
				err = json.Unmarshal(r.Body(), &b)
				return err != nil || b["ok"] != true
			}),
	}

	WithClientID(applicationConfig.PortClientId)(c)
	WithClientSecret(applicationConfig.PortClientSecret)(c)
	WithHeader("User-Agent", fmt.Sprintf("port-k8s-exporter/^0.3.4 (statekey/%s)", applicationConfig.StateKey))(c)

	for _, opt := range opts {
		opt(c)
	}

	return c
}

func (c *PortClient) ClearAuthToken() {
	// Setting an empty token will remove the Authorization header from the request (see pkg/mod/github.com/go-resty/resty/v2@v2.7.0/middleware.go:255)
	c.Client.SetAuthToken("")
}

func (c *PortClient) Authenticate(ctx context.Context, clientID, clientSecret string) (string, error) {
	url := "v1/auth/access_token"

	// If the request to v1/auth/access_token is sent with an invalid token, traefik will return a 401 error.
	// Since this is a public endpoint, we clear the existing token (if it does) to ensure the request is sent without it.
	c.ClearAuthToken()

	resp, err := c.Client.R().
		SetBody(map[string]interface{}{
			"clientId":     clientID,
			"clientSecret": clientSecret,
		}).
		SetContext(ctx).
		Post(url)
	if err != nil {
		return "", err
	}
	if resp.StatusCode() != 200 {
		return "", fmt.Errorf("failed to authenticate, got: %s", resp.Body())
	}

	var tokenResp port.AccessTokenResponse
	err = json.Unmarshal(resp.Body(), &tokenResp)
	if err != nil {
		return "", err
	}
	c.Client.SetAuthToken(tokenResp.AccessToken)
	return tokenResp.AccessToken, nil
}

func WithHeader(key, val string) Option {
	return func(pc *PortClient) {
		pc.Client.SetHeader(key, val)
	}
}

func WithClientID(clientID string) Option {
	return func(pc *PortClient) {
		pc.ClientID = clientID
	}
}

func WithClientSecret(clientSecret string) Option {
	return func(pc *PortClient) {
		pc.ClientSecret = clientSecret
	}
}

func WithDeleteDependents(deleteDependents bool) Option {
	return func(pc *PortClient) {
		pc.DeleteDependents = deleteDependents
	}
}

func WithCreateMissingRelatedEntities(createMissingRelatedEntities bool) Option {
	return func(pc *PortClient) {
		pc.CreateMissingRelatedEntities = createMissingRelatedEntities
	}
}
