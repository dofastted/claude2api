package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/dofastted/claude2api/internal/config"
	"github.com/dofastted/claude2api/internal/pkg/clientidentity"
)

const (
	defaultVersionFetchInterval = time.Hour
	versionFetchTimeout         = 30 * time.Second
	defaultNPMRegistryBaseURL   = "https://registry.npmjs.org"
	defaultCodexReleaseURL      = "https://api.github.com/repos/openai/codex/releases/latest"
)

type VersionFetcherService struct {
	registry *clientidentity.Registry
	cfg      *config.Config
	client   *http.Client
	npmURL   string
	codexURL string
	stopCh   chan struct{}
	stopOnce sync.Once
}

func NewVersionFetcherService(registry *clientidentity.Registry, cfg *config.Config) *VersionFetcherService {
	return &VersionFetcherService{
		registry: registry,
		cfg:      cfg,
		client:   http.DefaultClient,
		npmURL:   defaultNPMRegistryBaseURL,
		codexURL: defaultCodexReleaseURL,
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
	var wg sync.WaitGroup
	var claudeVer, claudeSDKVer, codexVer string
	var claudeErr, codexErr error

	wg.Add(2)

	go func() {
		defer wg.Done()
		claudeVer, claudeSDKVer, claudeErr = s.fetchClaudeVersions(ctx)
	}()

	go func() {
		defer wg.Done()
		codexVer, codexErr = s.fetchCodexVersion(ctx)
	}()

	wg.Wait()

	if claudeErr != nil || codexErr != nil {
		slog.Warn("version_fetcher_discard_update", "claude_error", claudeErr, "codex_error", codexErr)
		return "", "", ""
	}

	return claudeVer, claudeSDKVer, codexVer
}

func (s *VersionFetcherService) fetchClaudeVersions(ctx context.Context) (cliVersion, sdkVersion string, err error) {
	cliVersion, err = s.fetchNPMLatest(ctx, "@anthropic-ai/claude-code")
	if err != nil {
		return "", "", err
	}

	sdkVersion, err = s.fetchClaudeCodeSDKDependency(ctx, cliVersion)
	if err == nil {
		return cliVersion, sdkVersion, nil
	}

	sdkVersion, err = s.fetchNPMLatest(ctx, "@anthropic-ai/sdk")
	if err != nil {
		return "", "", err
	}

	return cliVersion, sdkVersion, nil
}

func (s *VersionFetcherService) fetchCodexVersion(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.codexURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "claude2api-version-fetcher")

	resp, err := s.httpClient().Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("github API status %d", resp.StatusCode)
	}

	var release struct {
		TagName    string `json:"tag_name"`
		Prerelease bool   `json:"prerelease"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", err
	}
	if release.Prerelease {
		return "", fmt.Errorf("latest release is prerelease")
	}

	version, err := extractStableVersion(release.TagName)
	if err != nil {
		return "", fmt.Errorf("invalid codex release tag %q: %w", release.TagName, err)
	}
	return version, nil
}

func (s *VersionFetcherService) fetchNPMLatest(ctx context.Context, pkg string) (string, error) {
	url := strings.TrimRight(s.npmURL, "/") + "/" + pkg
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := s.httpClient().Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("npm registry status %d", resp.StatusCode)
	}

	var data struct {
		DistTags map[string]string `json:"dist-tags"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", err
	}

	latest, ok := data.DistTags["latest"]
	if !ok || strings.TrimSpace(latest) == "" {
		return "", fmt.Errorf("no latest tag")
	}

	version, err := extractStableVersion(latest)
	if err != nil {
		return "", fmt.Errorf("invalid npm latest %q: %w", latest, err)
	}
	return version, nil
}

func (s *VersionFetcherService) fetchClaudeCodeSDKDependency(ctx context.Context, cliVersion string) (string, error) {
	url := strings.TrimRight(s.npmURL, "/") + "/@anthropic-ai/claude-code/" + cliVersion
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := s.httpClient().Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("npm package status %d", resp.StatusCode)
	}

	var data struct {
		Dependencies map[string]string `json:"dependencies"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", err
	}

	sdkVer, ok := data.Dependencies["@anthropic-ai/sdk"]
	if !ok || strings.TrimSpace(sdkVer) == "" {
		return "", fmt.Errorf("no sdk dependency")
	}

	version, err := extractStableVersion(sdkVer)
	if err != nil {
		return "", fmt.Errorf("invalid sdk dependency %q: %w", sdkVer, err)
	}
	return version, nil
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
		"User-Agent": "codex_cli_rs/" + cliVer + " (Ubuntu 22.4.0; x86_64) xterm-256color",
		"originator": "codex_cli_rs",
	}
}

func (s *VersionFetcherService) httpClient() *http.Client {
	if s != nil && s.client != nil {
		return s.client
	}
	return http.DefaultClient
}

var versionTokenPattern = regexp.MustCompile(`v?\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.-]+)?`)

func extractStableVersion(value string) (string, error) {
	token := versionTokenPattern.FindString(strings.TrimSpace(value))
	if token == "" {
		return "", fmt.Errorf("no semver token")
	}
	if strings.ContainsAny(token, "-+") {
		return "", fmt.Errorf("prerelease or build metadata version")
	}
	return strings.TrimPrefix(token, "v"), nil
}
