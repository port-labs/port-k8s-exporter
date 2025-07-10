package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"golang.org/x/oauth2"
	"strings"
	"sync"

	"github.com/go-resty/resty/v2"
	"github.com/port-labs/port-k8s-exporter/pkg/config"
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

var (
	cachedTokenSource oauth2.TokenSource
	tokenSourceMu     sync.RWMutex
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

func (c *PortClient) Authenticate(ctx context.Context, clientID, clientSecret string) (string, error) {
	token, err := getToken(clientID, clientSecret, c.Client.BaseURL)

	if err != nil {
		return "", fmt.Errorf("error getting token: %s", err.Error())
	}
	c.Client.SetAuthToken(token.AccessToken)
	return token.AccessToken, nil
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

func getToken(clientID, clientSecret, baseURL string) (*oauth2.Token, error) {
	tokenSourceMu.RLock()
	if cachedTokenSource != nil {
		tokenSourceMu.RUnlock()
		return cachedTokenSource.Token()
	}
	tokenSourceMu.RUnlock()

	tokenSourceMu.Lock()
	defer tokenSourceMu.Unlock()
	if cachedTokenSource == nil {
		raw := newTokenSource(clientID, clientSecret, baseURL)
		cachedTokenSource = oauth2.ReuseTokenSource(nil, raw)
	}
	return cachedTokenSource.Token()
}
