package service

import "github.com/Wei-Shaw/sub2api/internal/pkg/clientidentity"

func defaultCodexTestUserAgent() string {
	return clientidentity.NewRegistry().Get().Codex.Headers["User-Agent"]
}

func defaultCodexTestVersion() string {
	return clientidentity.NewRegistry().Get().Codex.VersionFields.CLIVersion
}
