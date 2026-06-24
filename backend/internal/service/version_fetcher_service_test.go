package service

import (
	"context"
	"net/http"
	"net/http/httptest"
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
	svc := NewVersionFetcherService(registry, cfg)

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
	t.Run("returns stable version and trims v prefix", func(t *testing.T) {
		svc, server := newVersionFetcherTestService(t, func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/repos/openai/codex/releases/latest", r.URL.Path)
			assert.Equal(t, "application/vnd.github.v3+json", r.Header.Get("Accept"))
			assert.Equal(t, "claude2api-version-fetcher", r.Header.Get("User-Agent"))
			_, _ = w.Write([]byte(`{"tag_name":"v0.125.0","prerelease":false}`))
		})
		defer server.Close()

		got, err := svc.fetchCodexVersion(context.Background())
		require.NoError(t, err)
		assert.Equal(t, "0.125.0", got)
	})

	t.Run("rejects prerelease flag", func(t *testing.T) {
		svc, server := newVersionFetcherTestService(t, func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"tag_name":"v0.126.0-beta.1","prerelease":true}`))
		})
		defer server.Close()

		_, err := svc.fetchCodexVersion(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "prerelease")
	})

	t.Run("rejects prerelease tag", func(t *testing.T) {
		svc, server := newVersionFetcherTestService(t, func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"tag_name":"v0.126.0-rc.1","prerelease":false}`))
		})
		defer server.Close()

		_, err := svc.fetchCodexVersion(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "prerelease")
	})

	t.Run("rejects non 200", func(t *testing.T) {
		svc, server := newVersionFetcherTestService(t, func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "not found", http.StatusNotFound)
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

func TestFetchVersionsDiscardsWhenAnySourceFails(t *testing.T) {
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
	assert.Empty(t, claudeVersion)
	assert.Empty(t, claudeSDKVersion)
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
	svc := NewVersionFetcherService(clientidentity.NewRegistry(), &config.Config{})
	svc.client = server.Client()
	svc.npmURL = server.URL
	svc.codexURL = server.URL + "/repos/openai/codex/releases/latest"
	return svc, server
}
