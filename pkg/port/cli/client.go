package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/port-labs/port-k8s-exporter/pkg/config"
	"github.com/port-labs/port-k8s-exporter/pkg/port"
)

type Authenticator struct {
	ClientID     string
	ClientSecret string
	AccessToken  string
	ExpiresIn    int
	AuthMutex    sync.Mutex
	LastRefresh  time.Time
}

const (
	AuthTokenEndpoint string = "v1/auth/access_token"
)

func NewAuthenticator(clientID, clientSecret string) *Authenticator {
	return &Authenticator{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		AuthMutex:    sync.Mutex{},
		LastRefresh:  time.Now(),
	}
}

func (a *Authenticator) AuthenticateClient(ctx context.Context, client *PortClient) (string, int, error) {
	a.AuthMutex.Lock()
	defer a.AuthMutex.Unlock()
	client.ClearAuthToken()
	if time.Since(a.LastRefresh) > time.Duration(a.ExpiresIn)*time.Second {
		_, _, err := a.refreshAccessToken(ctx, client)
		if err != nil {
			return "", 0, err
		}
	}
	client.Client.SetAuthToken(a.AccessToken)
	return a.AccessToken, a.ExpiresIn, nil
}

func (a *Authenticator) refreshAccessToken(ctx context.Context, client *PortClient) (string, int, error) {
	tokenResp, err := a.getAccessTokenResponse(ctx, client)
	if err != nil {
		return a.AccessToken, a.ExpiresIn, err
	}
	a.AccessToken = tokenResp.AccessToken
	a.ExpiresIn = int(tokenResp.ExpiresIn)
	a.LastRefresh = time.Now()
	return a.AccessToken, a.ExpiresIn, nil
}

func (a *Authenticator) getAccessTokenResponse(ctx context.Context, client *PortClient) (*port.AccessTokenResponse, error) {
	resp, err := client.Client.R().
		SetBody(map[string]interface{}{
			"clientId":     a.ClientID,
			"clientSecret": a.ClientSecret,
		}).
		SetContext(ctx).
		Post(AuthTokenEndpoint)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode() != 200 {
		return nil, fmt.Errorf("failed to authenticate, got: %s", resp.Body())
	}

	var tokenResp port.AccessTokenResponse
	err = json.Unmarshal(resp.Body(), &tokenResp)
	if err != nil {
		return nil, err
	}
	return &tokenResp, nil
}

type (
	Option     func(*PortClient)
	PortClient struct {
		Client                       *resty.Client
		ClientID                     string
		ClientSecret                 string
		DeleteDependents             bool
		CreateMissingRelatedEntities bool
		Authenticator                *Authenticator
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

	// Add pre-request hook to wait for Ready state
	c.Client.OnBeforeRequest(func(client *resty.Client, request *resty.Request) error {
		if request.Method == "POST" && strings.Contains(request.URL, AuthTokenEndpoint) {
			return nil
		}
		if c.Authenticator == nil {
			c.Authenticator = NewAuthenticator(c.ClientID, c.ClientSecret)
		}
		_, _, err := c.Authenticator.AuthenticateClient(context.Background(), c)
		return err
	})

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
