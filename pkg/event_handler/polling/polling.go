package polling

import (
	"errors"
	"os"
	"os/signal"
	"slices"
	"syscall"
	"time"

	"github.com/port-labs/port-k8s-exporter/pkg/logger"
	"github.com/port-labs/port-k8s-exporter/pkg/port"
	"github.com/port-labs/port-k8s-exporter/pkg/port/cli"
	"github.com/port-labs/port-k8s-exporter/pkg/port/org_details"
)

type ITicker interface {
	GetC() <-chan time.Time
}

type Ticker struct {
	ticker *time.Ticker
}

func NewTicker(d time.Duration) *Ticker {
	return &Ticker{
		ticker: time.NewTicker(d),
	}
}

func (t *Ticker) Stop() {
	t.ticker.Stop()
}

func (t *Ticker) GetC() <-chan time.Time {
	return t.ticker.C
}

type Handler struct {
	ticker                           ITicker
	stateKey                         string
	portClient                       *cli.PortClient
	pollingRate                      uint
	lastIntegrationStateUpdatedAt    string
	lastResyncRequestUpdatedAt            string
	resyncRequestsPollingEnabled          *bool
}

func NewPollingHandler(pollingRate uint, stateKey string, portClient *cli.PortClient, tickerOverride ITicker) *Handler {
	ticker := tickerOverride
	if ticker == nil {
		ticker = NewTicker(time.Second * time.Duration(pollingRate))
	}
	rv := &Handler{
		ticker:      ticker,
		stateKey:    stateKey,
		portClient:  portClient,
		pollingRate: pollingRate,
	}
	return rv
}

func formatUpdatedAt(updatedAt *time.Time) string {
	if updatedAt == nil {
		return ""
	}
	return updatedAt.UTC().Format(time.RFC3339Nano)
}

func shouldResync(lastUpdatedAt string, lastIntegrationStateUpdatedAt string) bool {
	if lastUpdatedAt == "" {
		return false
	}
	
	if lastIntegrationStateUpdatedAt == "" {
		return true
	}
	return lastIntegrationStateUpdatedAt != lastUpdatedAt
}

func shouldResyncFromResyncRequest(resyncRequestUpdatedAt string, lastProcessedResyncRequestUpdatedAt string) bool {
	if resyncRequestUpdatedAt == "" {
		return false
	}

	resyncRequestTime, err := parsePortTimestamp(resyncRequestUpdatedAt)
	if err != nil {
		return false
	}

	if lastProcessedResyncRequestUpdatedAt == "" {
		return true
	}

	lastProcessedTime, err := parsePortTimestamp(lastProcessedResyncRequestUpdatedAt)
	if err != nil {
		return true
	}

	return lastProcessedTime.Before(resyncRequestTime)
}

func parsePortTimestamp(value string) (time.Time, error) {
	layouts := []string{time.RFC3339Nano, time.RFC3339}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, errors.New("invalid timestamp format")
}

func (h *Handler) isResyncRequestsPollingEnabled() bool {
	if h.resyncRequestsPollingEnabled != nil {
		return *h.resyncRequestsPollingEnabled
	}

	flags, err := org_details.GetOrganizationFeatureFlags(h.portClient)
	if err != nil {
		logger.Errorw(
			"Failed to fetch organization feature flags for resync request polling, skipping resync request polling for this iteration",
			"error", err.Error(),
		)
		return false
	}

	enabled := slices.Contains(flags, port.OrgOceanPollingIntegrationResyncRequestsEnabledFeatureFlag)
	h.resyncRequestsPollingEnabled = &enabled
	return enabled
}

func (h *Handler) pollIteration(resync func()) {
	logger.Infof("Polling event listener iteration after %d seconds. Checking for changes...", h.pollingRate)

	integration, err := h.portClient.GetIntegrationForPolling(h.stateKey)
	if err != nil {
		logger.Errorw(
			"Failed to fetch current integration in polling listener, skipping iteration",
			"error", err.Error(),
		)
		return
	}

	lastUpdatedAt := formatUpdatedAt(integration.UpdatedAt)
	shouldTriggerResync := shouldResync(lastUpdatedAt, h.lastIntegrationStateUpdatedAt)
	resyncReason := "Detected change in integration, resyncing"
	resyncRequestUpdatedAt := ""

	if !shouldTriggerResync && h.isResyncRequestsPollingEnabled() {
		resyncRequest, err := h.portClient.GetIntegrationResyncRequest(h.stateKey)
		if err != nil {
			logger.Errorw(
				"Failed to fetch integration resync request in polling listener, continuing without resync request signal",
				"error", err.Error(),
			)
		} else if resyncRequest != nil {
			resyncRequestUpdatedAt = formatUpdatedAt(resyncRequest.UpdatedAt)
			shouldTriggerResync = shouldResyncFromResyncRequest(
				resyncRequestUpdatedAt,
				h.lastResyncRequestUpdatedAt,
			)
			if shouldTriggerResync {
				resyncReason = "Detected integration resync request"
			}
		}
	}

	if !shouldTriggerResync {
		return
	}

	logger.Info(resyncReason)
	h.lastIntegrationStateUpdatedAt = lastUpdatedAt
	if resyncRequestUpdatedAt != "" {
		h.lastResyncRequestUpdatedAt = resyncRequestUpdatedAt
	}
	resync()
}

func (h *Handler) Run(resync func()) {
	logger.Infof("Starting polling handler")

	integration, err := h.portClient.GetIntegrationForPolling(h.stateKey)
	if err != nil {
		logger.Errorf("Error fetching the first integration state: %s", err.Error())
	} else {
		h.lastIntegrationStateUpdatedAt = formatUpdatedAt(integration.UpdatedAt)
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	logger.Infof("Polling handler started")
	run := true
	for run {
		select {
		case sig := <-sigChan:
			logger.Infof("Received signal %v: terminating\n", sig)
			logger.Shutdown()
			run = false
		case <-h.ticker.GetC():
			h.pollIteration(resync)
		}
	}
}
