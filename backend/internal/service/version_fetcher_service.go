package service

import (
	"context"
	"encoding/json"
	"errors"
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
	defaultCodexNPMPackage      = "@openai/codex"
	defaultCodexReleaseURL      = "https://api.github.com/repos/openai/codex/releases/latest"
)

type VersionFetcherService struct {
	registry    *clientidentity.Registry
	cfg         *config.Config
	settingRepo SettingRepository
	client      *http.Client
	npmURL      string
	codexURL    string
	stopCh      chan struct{}
	stopOnce    sync.Once
}

func NewVersionFetcherService(registry *clientidentity.Registry, cfg *config.Config, settingRepo SettingRepository) *VersionFetcherService {
	return &VersionFetcherService{
		registry:    registry,
		cfg:         cfg,
		settingRepo: settingRepo,
		client:      http.DefaultClient,
		npmURL:      defaultNPMRegistryBaseURL,
		codexURL:    defaultCodexReleaseURL,
		stopCh:      make(chan struct{}),
	}
}

// Start runs the periodic version fetch loop only when ua_auto_fetch is enabled.
func (s *VersionFetcherService) Start() {
	if s == nil || s.cfg == nil || !s.cfg.Gateway.UAAutoFetch.Enabled {
		return
	}

	// 启动即用上次持久化的版本回灌 registry，避免进程重启后退回硬编码默认值、
	// 也不必等满一个 interval 才首次拉取。
	s.bootstrapFromDB()

	interval := s.cfg.Gateway.UAAutoFetch.Interval
	if interval == 0 {
		interval = defaultVersionFetchInterval
	}

	go func() {
		// 启动后立即拉取一次，刷新为最新版本；随后按 ticker 周期更新。
		s.fetchAndUpdate()
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

	// 拆分“全有或全无”：claude 与 codex 各自成功各自合并进当前快照并持久化，
	// 避免一侧失败导致另一侧的新版本也被丢弃。
	if claudeVersion == "" && codexVersion == "" {
		return
	}

	current := s.registry.Get()
	claude := current.Claude
	codex := current.Codex

	if claudeVersion != "" {
		if claudeSDKVersion == "" {
			claudeSDKVersion = claude.VersionFields.SDKVersion
		}
		claude = clientidentity.ClaudeSnapshot{
			Headers: s.buildClaudeHeaders(claudeVersion, claudeSDKVersion),
			VersionFields: clientidentity.VersionFields{
				CLIVersion: claudeVersion,
				SDKVersion: claudeSDKVersion,
				CCVersion:  claudeVersion,
			},
			TLSProfileName: clientidentity.TLSProfileClaudeCLIDefault,
		}
	}
	if codexVersion != "" {
		codex = clientidentity.CodexSnapshot{
			Headers: s.buildCodexHeaders(codexVersion),
			VersionFields: clientidentity.VersionFields{
				CLIVersion: codexVersion,
			},
			TLSProfileName: clientidentity.TLSProfileCodexCLIDefault,
		}
	}

	s.registry.Swap(&clientidentity.Snapshots{Claude: claude, Codex: codex})
	s.persistVersions(ctx, claudeVersion, claudeSDKVersion, codexVersion)
}

// bootstrapFromDB 在启动时把 DB 里持久化的版本回灌 registry，使进程一启动即用
// 上次拉取到的版本，无需等待首次 ticker 触发。任一字段缺失则保留硬编码默认。
func (s *VersionFetcherService) bootstrapFromDB() {
	if s == nil || s.registry == nil || s.settingRepo == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), versionFetchTimeout)
	defer cancel()

	claudeVersion, claudeSDKVersion, codexVersion := s.loadPersistedVersions(ctx)
	if claudeVersion == "" && codexVersion == "" {
		return
	}

	current := s.registry.Get()
	claude := current.Claude
	codex := current.Codex

	if claudeVersion != "" {
		if claudeSDKVersion == "" {
			claudeSDKVersion = claude.VersionFields.SDKVersion
		}
		claude = clientidentity.ClaudeSnapshot{
			Headers: s.buildClaudeHeaders(claudeVersion, claudeSDKVersion),
			VersionFields: clientidentity.VersionFields{
				CLIVersion: claudeVersion,
				SDKVersion: claudeSDKVersion,
				CCVersion:  claudeVersion,
			},
			TLSProfileName: clientidentity.TLSProfileClaudeCLIDefault,
		}
	}
	if codexVersion != "" {
		codex = clientidentity.CodexSnapshot{
			Headers: s.buildCodexHeaders(codexVersion),
			VersionFields: clientidentity.VersionFields{
				CLIVersion: codexVersion,
			},
			TLSProfileName: clientidentity.TLSProfileCodexCLIDefault,
		}
	}

	s.registry.Swap(&clientidentity.Snapshots{Claude: claude, Codex: codex})
	slog.Info("version_fetcher_bootstrap_from_db", "claude", claudeVersion, "claude_sdk", claudeSDKVersion, "codex", codexVersion)
}

// persistVersions 把本次拉取到的版本写入 setting 表，空值跳过对应 key。
// 写入失败仅告警，不影响内存快照。
func (s *VersionFetcherService) persistVersions(ctx context.Context, claudeVer, claudeSDKVer, codexVer string) {
	if s == nil || s.settingRepo == nil {
		return
	}
	if claudeVer != "" {
		payload, err := json.Marshal(struct {
			CLI string `json:"cli"`
			SDK string `json:"sdk"`
		}{CLI: claudeVer, SDK: claudeSDKVer})
		if err == nil {
			if err := s.settingRepo.Set(ctx, SettingKeyClaudeCLIVersion, string(payload)); err != nil {
				slog.Warn("version_fetcher_persist_claude_failed", "error", err)
			}
		}
	}
	if codexVer != "" {
		if err := s.settingRepo.Set(ctx, SettingKeyCodexCLIVersion, codexVer); err != nil {
			slog.Warn("version_fetcher_persist_codex_failed", "error", err)
		}
	}
}

// loadPersistedVersions 从 setting 表读取持久化版本。缺失（ErrSettingNotFound）视为空值。
func (s *VersionFetcherService) loadPersistedVersions(ctx context.Context) (claudeVer, claudeSDKVer, codexVer string) {
	if s == nil || s.settingRepo == nil {
		return "", "", ""
	}

	if raw, err := s.settingRepo.GetValue(ctx, SettingKeyClaudeCLIVersion); err == nil {
		var parsed struct {
			CLI string `json:"cli"`
			SDK string `json:"sdk"`
		}
		if json.Unmarshal([]byte(raw), &parsed) == nil {
			claudeVer = strings.TrimSpace(parsed.CLI)
			claudeSDKVer = strings.TrimSpace(parsed.SDK)
		}
	} else if !errors.Is(err, ErrSettingNotFound) {
		slog.Warn("version_fetcher_load_claude_failed", "error", err)
	}

	if raw, err := s.settingRepo.GetValue(ctx, SettingKeyCodexCLIVersion); err == nil {
		codexVer = strings.TrimSpace(raw)
	} else if !errors.Is(err, ErrSettingNotFound) {
		slog.Warn("version_fetcher_load_codex_failed", "error", err)
	}
	return claudeVer, claudeSDKVer, codexVer
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

	// 拆分后的语义：各自返回各自拉到的版本，失败侧返回空串。
	// 仅在两侧都失败时记录一次诊断日志，调用方据空串决定是否跳过 Swap/持久化。
	if claudeErr != nil {
		slog.Warn("version_fetcher_claude_failed", "error", claudeErr)
		claudeVer, claudeSDKVer = "", ""
	}
	if codexErr != nil {
		slog.Warn("version_fetcher_codex_failed", "error", codexErr)
		codexVer = ""
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
	version, npmErr := s.fetchNPMLatest(ctx, defaultCodexNPMPackage)
	if npmErr == nil {
		return version, nil
	}

	version, releaseErr := s.fetchCodexReleaseVersion(ctx)
	if releaseErr == nil {
		return version, nil
	}
	return "", fmt.Errorf("npm latest: %v; github release: %w", npmErr, releaseErr)
}

func (s *VersionFetcherService) fetchCodexReleaseVersion(ctx context.Context) (string, error) {
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
