package service

import "github.com/dofastted/claude2api/internal/pkg/clientidentity"

func defaultCodexTestUserAgent() string {
	return clientidentity.NewRegistry().Get().Codex.Headers["User-Agent"]
}

func defaultCodexTestVersion() string {
	return clientidentity.NewRegistry().Get().Codex.VersionFields.CLIVersion
}
