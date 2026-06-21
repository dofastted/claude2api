package service

import (
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/clientidentity"
	"github.com/stretchr/testify/assert"
)

func TestVersionFetcherDisabledDoesNotStart(t *testing.T) {
	cfg := &config.Config{
		Gateway: config.GatewayConfig{
			UAAutoFetch: config.UAAutoFetchConfig{Enabled: false},
		},
	}
	registry := clientidentity.NewRegistry()
	initial := registry.Get()
	svc := NewVersionFetcherService(registry, cfg)

	svc.Start()
	time.Sleep(50 * time.Millisecond)

	assert.Same(t, initial, registry.Get())
}
