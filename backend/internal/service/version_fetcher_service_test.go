package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/dofastted/claude2api/internal/config"
	"github.com/dofastted/claude2api/internal/pkg/clientidentity"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVersionFetcherDisabledDoesNotStart(t *testing.T) {
	cfg := &config.Config{
		Gateway: config.GatewayConfig{
			UAAutoFetch: config.UAAutoFetchConfig{Enabled: false},
		},
	}
	registry := clientidentity.NewRegistry()
	initial := registry.Get()
	svc := NewVersionFetcherService(registry, cfg, nil)

	svc.Start()
	time.Sleep(50 * time.Millisecond)

	assert.Same(t, initial, registry.Get())
}

func TestFetchNPMLatest(t *testing.T) {
	t.Run("returns latest", func(t *testing.T) {
		svc, server := newVersionFetcherTestService(t, func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/@anthropic-ai/claude-code", r.URL.Path)
			assert.Equal(t, "application/json", r.Header.Get("Accept"))
			_, _ = w.Write([]byte(`{"dist-tags":{"latest":"2.1.161"}}`))
		})
		defer server.Close()

		got, err := svc.fetchNPMLatest(context.Background(), "@anthropic-ai/claude-code")
		require.NoError(t, err)
		assert.Equal(t, "2.1.161", got)
	})

	t.Run("rejects non 200", func(t *testing.T) {
		svc, server := newVersionFetcherTestService(t, func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "not found", http.StatusNotFound)
		})
		defer server.Close()

		_, err := svc.fetchNPMLatest(context.Background(), "@anthropic-ai/claude-code")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "npm registry status 404")
	})

	t.Run("rejects missing latest", func(t *testing.T) {
		svc, server := newVersionFetcherTestService(t, func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"dist-tags":{"beta":"3.0.0-beta.1"}}`))
		})
		defer server.Close()

		_, err := svc.fetchNPMLatest(context.Background(), "@anthropic-ai/claude-code")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no latest tag")
	})

	t.Run("rejects prerelease latest", func(t *testing.T) {
		svc, server := newVersionFetcherTestService(t, func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"dist-tags":{"latest":"3.0.0-beta.1"}}`))
		})
		defer server.Close()

		_, err := svc.fetchNPMLatest(context.Background(), "@anthropic-ai/claude-code")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "prerelease")
	})

	t.Run("honors context timeout", func(t *testing.T) {
		svc, server := newVersionFetcherTestService(t, func(w http.ResponseWriter, r *http.Request) {
			<-r.Context().Done()
			w.WriteHeader(http.StatusGatewayTimeout)
		})
		defer server.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		defer cancel()

		_, err := svc.fetchNPMLatest(ctx, "@anthropic-ai/claude-code")
		require.Error(t, err)
		assert.ErrorIs(t, err, context.DeadlineExceeded)
	})
}

func TestFetchCodexVersion(t *testing.T) {
	t.Run("returns npm latest", func(t *testing.T) {
		svc, server := newVersionFetcherTestService(t, func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/@openai/codex", r.URL.Path)
			assert.Equal(t, "application/json", r.Header.Get("Accept"))
			_, _ = w.Write([]byte(`{"dist-tags":{"latest":"0.142.2"}}`))
		})
		defer server.Close()

		got, err := svc.fetchCodexVersion(context.Background())
		require.NoError(t, err)
		assert.Equal(t, "0.142.2", got)
	})

	t.Run("falls back to github rust release tag", func(t *testing.T) {
		svc, server := newVersionFetcherTestService(t, func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/@openai/codex":
				http.Error(w, "npm outage", http.StatusBadGateway)
			case "/repos/openai/codex/releases/latest":
				assert.Equal(t, "application/vnd.github.v3+json", r.Header.Get("Accept"))
				assert.Equal(t, "claude2api-version-fetcher", r.Header.Get("User-Agent"))
				_, _ = w.Write([]byte(`{"tag_name":"rust-v0.142.2","prerelease":false}`))
			default:
				http.NotFound(w, r)
			}
		})
		defer server.Close()

		got, err := svc.fetchCodexVersion(context.Background())
		require.NoError(t, err)
		assert.Equal(t, "0.142.2", got)
	})

	t.Run("rejects prerelease flag", func(t *testing.T) {
		svc, server := newVersionFetcherTestService(t, func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/@openai/codex":
				http.Error(w, "npm outage", http.StatusBadGateway)
			case "/repos/openai/codex/releases/latest":
				_, _ = w.Write([]byte(`{"tag_name":"v0.126.0-beta.1","prerelease":true}`))
			default:
				http.NotFound(w, r)
			}
		})
		defer server.Close()

		_, err := svc.fetchCodexVersion(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "prerelease")
	})

	t.Run("rejects prerelease tag", func(t *testing.T) {
		svc, server := newVersionFetcherTestService(t, func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/@openai/codex":
				http.Error(w, "npm outage", http.StatusBadGateway)
			case "/repos/openai/codex/releases/latest":
				_, _ = w.Write([]byte(`{"tag_name":"v0.126.0-rc.1","prerelease":false}`))
			default:
				http.NotFound(w, r)
			}
		})
		defer server.Close()

		_, err := svc.fetchCodexVersion(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "prerelease")
	})

	t.Run("rejects non 200", func(t *testing.T) {
		svc, server := newVersionFetcherTestService(t, func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/@openai/codex":
				http.Error(w, "npm outage", http.StatusBadGateway)
			case "/repos/openai/codex/releases/latest":
				http.Error(w, "not found", http.StatusNotFound)
			default:
				http.NotFound(w, r)
			}
		})
		defer server.Close()

		_, err := svc.fetchCodexVersion(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "github API status 404")
	})
}

func TestFetchClaudeCodeSDKDependency(t *testing.T) {
	t.Run("returns dependency version and trims range prefix", func(t *testing.T) {
		svc, server := newVersionFetcherTestService(t, func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/@anthropic-ai/claude-code/2.1.161", r.URL.Path)
			_, _ = w.Write([]byte(`{"dependencies":{"@anthropic-ai/sdk":"^0.62.0"}}`))
		})
		defer server.Close()

		got, err := svc.fetchClaudeCodeSDKDependency(context.Background(), "2.1.161")
		require.NoError(t, err)
		assert.Equal(t, "0.62.0", got)
	})

	t.Run("trims tilde range prefix", func(t *testing.T) {
		svc, server := newVersionFetcherTestService(t, func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"dependencies":{"@anthropic-ai/sdk":"~0.61.0"}}`))
		})
		defer server.Close()

		got, err := svc.fetchClaudeCodeSDKDependency(context.Background(), "2.1.161")
		require.NoError(t, err)
		assert.Equal(t, "0.61.0", got)
	})

	t.Run("rejects missing sdk dependency", func(t *testing.T) {
		svc, server := newVersionFetcherTestService(t, func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"dependencies":{"other":"1.0.0"}}`))
		})
		defer server.Close()

		_, err := svc.fetchClaudeCodeSDKDependency(context.Background(), "2.1.161")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no sdk dependency")
	})

	t.Run("rejects non 200", func(t *testing.T) {
		svc, server := newVersionFetcherTestService(t, func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "not found", http.StatusNotFound)
		})
		defer server.Close()

		_, err := svc.fetchClaudeCodeSDKDependency(context.Background(), "2.1.161")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "npm package status 404")
	})
}

func TestFetchClaudeVersionsFallsBackToSDKLatest(t *testing.T) {
	svc, server := newVersionFetcherTestService(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/@anthropic-ai/claude-code":
			_, _ = w.Write([]byte(`{"dist-tags":{"latest":"2.1.161"}}`))
		case "/@anthropic-ai/claude-code/2.1.161":
			http.Error(w, "missing package metadata", http.StatusNotFound)
		case "/@anthropic-ai/sdk":
			_, _ = w.Write([]byte(`{"dist-tags":{"latest":"0.62.0"}}`))
		default:
			http.NotFound(w, r)
		}
	})
	defer server.Close()

	cliVersion, sdkVersion, err := svc.fetchClaudeVersions(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "2.1.161", cliVersion)
	assert.Equal(t, "0.62.0", sdkVersion)
}

func TestFetchVersionsKeepsPartialSuccess(t *testing.T) {
	// 拆分“全有或全无”后：claude 拉取成功、codex 失败时，claude 侧仍应返回版本，codex 侧为空。
	svc, server := newVersionFetcherTestService(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/@anthropic-ai/claude-code":
			_, _ = w.Write([]byte(`{"dist-tags":{"latest":"2.1.161"}}`))
		case "/@anthropic-ai/claude-code/2.1.161":
			_, _ = w.Write([]byte(`{"dependencies":{"@anthropic-ai/sdk":"^0.62.0"}}`))
		case "/repos/openai/codex/releases/latest":
			http.Error(w, "github outage", http.StatusBadGateway)
		default:
			http.NotFound(w, r)
		}
	})
	defer server.Close()

	claudeVersion, claudeSDKVersion, codexVersion := svc.fetchVersions(context.Background())
	assert.Equal(t, "2.1.161", claudeVersion)
	assert.Equal(t, "0.62.0", claudeSDKVersion)
	assert.Empty(t, codexVersion)
}

func TestFetchAndUpdateBuildsConsistentCodexIdentity(t *testing.T) {
	svc, server := newVersionFetcherTestService(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/@anthropic-ai/claude-code":
			_, _ = w.Write([]byte(`{"dist-tags":{"latest":"2.1.161"}}`))
		case "/@anthropic-ai/claude-code/2.1.161":
			_, _ = w.Write([]byte(`{"dependencies":{"@anthropic-ai/sdk":"^0.62.0"}}`))
		case "/repos/openai/codex/releases/latest":
			_, _ = w.Write([]byte(`{"tag_name":"v0.125.0","prerelease":false}`))
		default:
			http.NotFound(w, r)
		}
	})
	defer server.Close()

	svc.fetchAndUpdate()

	snapshot := svc.registry.Get()
	assert.Equal(t, "0.125.0", snapshot.Codex.VersionFields.CLIVersion)
	assert.Equal(t, "codex_cli_rs", snapshot.Codex.Headers["originator"])
	assert.Equal(t, "codex_cli_rs/0.125.0 (Ubuntu 22.4.0; x86_64) xterm-256color", snapshot.Codex.Headers["User-Agent"])
}

func newVersionFetcherTestService(t *testing.T, handler http.HandlerFunc) (*VersionFetcherService, *httptest.Server) {
	t.Helper()

	server := httptest.NewServer(handler)
	svc := NewVersionFetcherService(clientidentity.NewRegistry(), &config.Config{}, nil)
	svc.client = server.Client()
	svc.npmURL = server.URL
	svc.codexURL = server.URL + "/repos/openai/codex/releases/latest"
	return svc, server
}

// memorySettingRepo 是 SettingRepository 的内存实现，用于测试持久化与启动加载。
type memorySettingRepo struct {
	mu   sync.Mutex
	data map[string]string
}

func newMemorySettingRepo() *memorySettingRepo {
	return &memorySettingRepo{data: map[string]string{}}
}

func (m *memorySettingRepo) Get(ctx context.Context, key string) (*Setting, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.data[key]
	if !ok {
		return nil, ErrSettingNotFound
	}
	return &Setting{Key: key, Value: v}, nil
}

func (m *memorySettingRepo) GetValue(ctx context.Context, key string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.data[key]
	if !ok {
		return "", ErrSettingNotFound
	}
	return v, nil
}

func (m *memorySettingRepo) Set(ctx context.Context, key, value string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[key] = value
	return nil
}

func (m *memorySettingRepo) GetMultiple(ctx context.Context, keys []string) (map[string]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[string]string{}
	for _, k := range keys {
		if v, ok := m.data[k]; ok {
			out[k] = v
		}
	}
	return out, nil
}

func (m *memorySettingRepo) SetMultiple(ctx context.Context, settings map[string]string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for k, v := range settings {
		m.data[k] = v
	}
	return nil
}

func (m *memorySettingRepo) GetAll(ctx context.Context) (map[string]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[string]string{}
	for k, v := range m.data {
		out[k] = v
	}
	return out, nil
}

func (m *memorySettingRepo) Delete(ctx context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, key)
	return nil
}

func TestFetchAndUpdatePersistsVersionsAndPartialSuccess(t *testing.T) {
	repo := newMemorySettingRepo()
	registry := clientidentity.NewRegistry()
	svc := NewVersionFetcherService(registry, &config.Config{}, repo)
	svc, server := withTestHTTP(svc, t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/@anthropic-ai/claude-code":
			_, _ = w.Write([]byte(`{"dist-tags":{"latest":"2.2.0"}}`))
		case "/@anthropic-ai/claude-code/2.2.0":
			_, _ = w.Write([]byte(`{"dependencies":{"@anthropic-ai/sdk":"^0.95.0"}}`))
		case "/repos/openai/codex/releases/latest":
			_, _ = w.Write([]byte(`{"tag_name":"v0.130.0","prerelease":false}`))
		default:
			http.NotFound(w, r)
		}
	})
	defer server.Close()

	svc.fetchAndUpdate()

	snapshot := registry.Get()
	assert.Equal(t, "2.2.0", snapshot.Claude.VersionFields.CLIVersion)
	assert.Equal(t, "0.95.0", snapshot.Claude.VersionFields.SDKVersion)
	assert.Equal(t, "0.130.0", snapshot.Codex.VersionFields.CLIVersion)

	// 持久化写入 setting 表。
	claudeRaw, err := repo.GetValue(context.Background(), SettingKeyClaudeCLIVersion)
	require.NoError(t, err)
	assert.Contains(t, claudeRaw, "2.2.0")
	assert.Contains(t, claudeRaw, "0.95.0")
	codexRaw, err := repo.GetValue(context.Background(), SettingKeyCodexCLIVersion)
	require.NoError(t, err)
	assert.Equal(t, "0.130.0", codexRaw)
}

func TestFetchAndUpdatePersistsCodexOnlyWhenClaudeFails(t *testing.T) {
	repo := newMemorySettingRepo()
	registry := clientidentity.NewRegistry()
	initialClaude := registry.Get().Claude
	svc := NewVersionFetcherService(registry, &config.Config{}, repo)
	svc, server := withTestHTTP(svc, t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/@anthropic-ai/claude-code":
			http.Error(w, "npm outage", http.StatusBadGateway)
		case "/repos/openai/codex/releases/latest":
			_, _ = w.Write([]byte(`{"tag_name":"v0.130.0","prerelease":false}`))
		default:
			http.NotFound(w, r)
		}
	})
	defer server.Close()

	svc.fetchAndUpdate()

	snapshot := registry.Get()
	// claude 侧保留默认快照未被覆盖。
	assert.Equal(t, initialClaude.VersionFields.CLIVersion, snapshot.Claude.VersionFields.CLIVersion)
	// codex 侧成功更新并持久化。
	assert.Equal(t, "0.130.0", snapshot.Codex.VersionFields.CLIVersion)
	_, err := repo.GetValue(context.Background(), SettingKeyCodexCLIVersion)
	require.NoError(t, err)
	_, err = repo.GetValue(context.Background(), SettingKeyClaudeCLIVersion)
	assert.ErrorIs(t, err, ErrSettingNotFound)
}

func TestBootstrapFromDBRestoresPersistedVersions(t *testing.T) {
	repo := newMemorySettingRepo()
	require.NoError(t, repo.Set(context.Background(), SettingKeyClaudeCLIVersion, `{"cli":"2.3.0","sdk":"0.96.0"}`))
	require.NoError(t, repo.Set(context.Background(), SettingKeyCodexCLIVersion, "0.140.0"))

	registry := clientidentity.NewRegistry()
	svc := NewVersionFetcherService(registry, &config.Config{}, repo)
	svc.bootstrapFromDB()

	snapshot := registry.Get()
	assert.Equal(t, "2.3.0", snapshot.Claude.VersionFields.CLIVersion)
	assert.Equal(t, "0.96.0", snapshot.Claude.VersionFields.SDKVersion)
	assert.Equal(t, "0.140.0", snapshot.Codex.VersionFields.CLIVersion)
}

func TestBootstrapFromDBNoOpWhenEmpty(t *testing.T) {
	repo := newMemorySettingRepo()
	registry := clientidentity.NewRegistry()
	initial := registry.Get()
	svc := NewVersionFetcherService(registry, &config.Config{}, repo)
	svc.bootstrapFromDB()
	assert.Same(t, initial, registry.Get())
}

// withTestHTTP 给已有 svc 装上指向测试 server 的 HTTP 客户端与 URL，返回 svc 与 server。
func withTestHTTP(svc *VersionFetcherService, t *testing.T, handler http.HandlerFunc) (*VersionFetcherService, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(handler)
	svc.client = server.Client()
	svc.npmURL = server.URL
	svc.codexURL = server.URL + "/repos/openai/codex/releases/latest"
	return svc, server
}
