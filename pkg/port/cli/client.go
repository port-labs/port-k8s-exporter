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

var cachedTokenSource oauth2.TokenSource
var tokenSourceMu sync.RWMutex

func New(applicationConfig *config.ApplicationConfiguration, opts ...Option) *PortClient {
	raw := newTokenSource(applicationConfig)
	cached := oauth2.ReuseTokenSource(nil, raw)
	authedHttpClient := oauth2.NewClient(context.Background(), cached)

	restyClient := resty.New().
		SetBaseURL(applicationConfig.PortBaseURL).
		SetTransport(authedHttpClient.Transport).
		SetRetryCount(5).
		SetRetryWaitTime(300).
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
		})
	c := &PortClient{
		Client:       restyClient,
		ClientID:     applicationConfig.PortClientId,
		ClientSecret: applicationConfig.PortClientSecret,
	}

	WithHeader("User-Agent", fmt.Sprintf("port-k8s-exporter/^0.3.4 (statekey/%s)", applicationConfig.StateKey))(c)

	for _, opt := range opts {
		opt(c)
	}

	return c
}

func (c *PortClient) Authenticate(ctx context.Context, clientID, clientSecret string) (string, error) {

	appCfg := &config.ApplicationConfiguration{
		PortClientId:     clientID,
		PortClientSecret: clientSecret,
		PortBaseURL:      c.Client.BaseURL,
	}
	token, err := getCachedToken(ctx, appCfg)

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

func getCachedToken(ctx context.Context, cfg *config.ApplicationConfiguration) (*oauth2.Token, error) {
	tokenSourceMu.RLock()
	if cachedTokenSource != nil {
		tokenSourceMu.RUnlock()
		return cachedTokenSource.Token()
	}
	tokenSourceMu.RUnlock()

	tokenSourceMu.Lock()
	defer tokenSourceMu.Unlock()
	if cachedTokenSource == nil {
		raw := newTokenSource(cfg)
		cachedTokenSource = oauth2.ReuseTokenSource(nil, raw)
	}
	return cachedTokenSource.Token()
}
