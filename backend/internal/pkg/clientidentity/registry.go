package clientidentity

import (
	"sync/atomic"

	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
)

const (
	defaultCodexCLIVersion = "0.125.0"
	defaultCodexCLIUA      = "codex_cli_rs/0.125.0 (Ubuntu 22.4.0; x86_64) xterm-256color"
)

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
	headers := cloneHeaders(claude.DefaultHeaders)
	return ClaudeSnapshot{
		Headers: headers,
		VersionFields: VersionFields{
			CLIVersion: claude.CLICurrentVersion,
			SDKVersion: headers["X-Stainless-Package-Version"],
			CCVersion:  claude.CLICurrentVersion,
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
