package polling

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestShouldResync(t *testing.T) {
	assert.False(t, shouldResync("2026-01-02T00:00:00Z", ""))
	assert.False(t, shouldResync("2026-01-02T00:00:00Z", "2026-01-02T00:00:00Z"))
	assert.True(t, shouldResync("2026-01-02T00:00:00Z", "2026-01-01T00:00:00Z"))
}

func TestShouldResyncFromResyncRequest(t *testing.T) {
	assert.False(t, shouldResyncFromResyncRequest("", ""))
	assert.False(t, shouldResyncFromResyncRequest("", "2026-01-01T00:00:00Z"))
	assert.False(t, shouldResyncFromResyncRequest("invalid", ""))
	assert.True(t, shouldResyncFromResyncRequest("2026-01-02T00:00:00Z", ""))
	assert.True(t, shouldResyncFromResyncRequest("2026-01-02T00:00:00Z", "2026-01-01T00:00:00Z"))
	assert.False(t, shouldResyncFromResyncRequest("2026-01-01T00:00:00Z", "2026-01-02T00:00:00Z"))
	assert.True(t, shouldResyncFromResyncRequest("2026-01-02T00:00:00Z", "invalid"))
}

func TestFormatUpdatedAt(t *testing.T) {
	assert.Equal(t, "", formatUpdatedAt(nil))

	ts := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	assert.Equal(t, "2026-01-01T00:00:00Z", formatUpdatedAt(&ts))
}
