package service

import (
	"context"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/clientidentity"
)

const (
	defaultVersionFetchInterval = time.Hour
	versionFetchTimeout         = 30 * time.Second
)

type VersionFetcherService struct {
	registry *clientidentity.Registry
	cfg      *config.Config
	stopCh   chan struct{}
	stopOnce sync.Once
}

func NewVersionFetcherService(registry *clientidentity.Registry, cfg *config.Config) *VersionFetcherService {
	return &VersionFetcherService{
		registry: registry,
		cfg:      cfg,
		stopCh:   make(chan struct{}),
	}
}

// Start runs the periodic version fetch loop only when ua_auto_fetch is enabled.
func (s *VersionFetcherService) Start() {
	if s == nil || s.cfg == nil || !s.cfg.Gateway.UAAutoFetch.Enabled {
		return
	}

	interval := s.cfg.Gateway.UAAutoFetch.Interval
	if interval == 0 {
		interval = defaultVersionFetchInterval
	}

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				s.fetchAndUpdate()
			case <-s.stopCh:
				return
			}
		}
	}()
}

func (s *VersionFetcherService) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		close(s.stopCh)
	})
}

func (s *VersionFetcherService) fetchAndUpdate() {
	if s == nil || s.registry == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), versionFetchTimeout)
	defer cancel()

	claudeVersion, claudeSDKVersion, codexVersion := s.fetchVersions(ctx)
	if claudeVersion == "" || claudeSDKVersion == "" || codexVersion == "" {
		return
	}

	s.registry.Swap(&clientidentity.Snapshots{
		Claude: clientidentity.ClaudeSnapshot{
			Headers: s.buildClaudeHeaders(claudeVersion, claudeSDKVersion),
			VersionFields: clientidentity.VersionFields{
				CLIVersion: claudeVersion,
				SDKVersion: claudeSDKVersion,
				CCVersion:  claudeVersion,
			},
			TLSProfileName: clientidentity.TLSProfileClaudeCLIDefault,
		},
		Codex: clientidentity.CodexSnapshot{
			Headers: s.buildCodexHeaders(codexVersion),
			VersionFields: clientidentity.VersionFields{
				CLIVersion: codexVersion,
			},
			TLSProfileName: clientidentity.TLSProfileCodexCLIDefault,
		},
	})
}

func (s *VersionFetcherService) fetchVersions(ctx context.Context) (claudeVersion, claudeSDKVersion, codexVersion string) {
	_ = ctx
	return "", "", ""
}

func (s *VersionFetcherService) buildClaudeHeaders(cliVer, sdkVer string) map[string]string {
	return map[string]string{
		"User-Agent":                  "claude-cli/" + cliVer + " (external, cli)",
		"X-Stainless-Package-Version": sdkVer,
		"X-App":                       "cli",
	}
}

func (s *VersionFetcherService) buildCodexHeaders(cliVer string) map[string]string {
	return map[string]string{
		"User-Agent": "codex_cli_rs/" + cliVer,
		"originator": "codex_cli_rs",
	}
}
