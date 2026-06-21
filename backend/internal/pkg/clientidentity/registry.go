package clientidentity

import (
	"sync/atomic"
)

const (
	defaultCodexCLIVersion = "0.125.0"
	defaultCodexCLIUA      = "codex_cli_rs/0.125.0 (Ubuntu 22.4.0; x86_64) xterm-256color"

	defaultClaudeCLIVersion = "2.1.161"
	defaultClaudeSDKVersion = "0.94.0"
)

var defaultClaudeHeaders = map[string]string{
	"User-Agent":                                "claude-cli/" + defaultClaudeCLIVersion + " (external, cli)",
	"X-Stainless-Lang":                          "js",
	"X-Stainless-Package-Version":               defaultClaudeSDKVersion,
	"X-Stainless-OS":                            "Linux",
	"X-Stainless-Arch":                          "arm64",
	"X-Stainless-Runtime":                       "node",
	"X-Stainless-Runtime-Version":               "v24.3.0",
	"X-Stainless-Retry-Count":                   "0",
	"X-Stainless-Timeout":                       "600",
	"X-App":                                     "cli",
	"Anthropic-Dangerous-Direct-Browser-Access": "true",
}

// Registry holds the active complete Claude and Codex identity snapshots.
type Registry struct {
	snapshotPtr atomic.Pointer[Snapshots]
}

// Snapshots is the complete baseline for both client families.
type Snapshots struct {
	Claude ClaudeSnapshot
	Codex  CodexSnapshot
}

// ClaudeSnapshot keeps all linked Claude identity fields together.
type ClaudeSnapshot struct {
	Headers        map[string]string
	VersionFields  VersionFields
	TLSProfileName string
}

// CodexSnapshot keeps all linked Codex identity fields together.
type CodexSnapshot struct {
	Headers        map[string]string
	VersionFields  VersionFields
	TLSProfileName string
}

func NewRegistry() *Registry {
	r := &Registry{}
	r.snapshotPtr.Store(&Snapshots{
		Claude: r.defaultClaudeSnapshot(),
		Codex:  r.defaultCodexSnapshot(),
	})
	return r
}

// Get returns the active immutable snapshot pointer atomically.
func (r *Registry) Get() *Snapshots {
	return r.snapshotPtr.Load()
}

// Swap atomically replaces both Claude and Codex snapshots as one unit.
func (r *Registry) Swap(snapshots *Snapshots) {
	if snapshots == nil {
		return
	}
	r.snapshotPtr.Store(snapshots)
}

func (r *Registry) defaultClaudeSnapshot() ClaudeSnapshot {
	headers := cloneHeaders(defaultClaudeHeaders)
	return ClaudeSnapshot{
		Headers: headers,
		VersionFields: VersionFields{
			CLIVersion: defaultClaudeCLIVersion,
			SDKVersion: headers["X-Stainless-Package-Version"],
			CCVersion:  defaultClaudeCLIVersion,
		},
		TLSProfileName: TLSProfileClaudeCLIDefault,
	}
}

func (r *Registry) defaultCodexSnapshot() CodexSnapshot {
	return CodexSnapshot{
		Headers: map[string]string{
			"User-Agent": defaultCodexCLIUA,
			"originator": "codex_cli_rs",
		},
		VersionFields: VersionFields{
			CLIVersion: defaultCodexCLIVersion,
		},
		TLSProfileName: TLSProfileCodexCLIDefault,
	}
}
