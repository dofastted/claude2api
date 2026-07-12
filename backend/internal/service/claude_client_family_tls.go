package service

import (
	"strings"

	"github.com/dofastted/claude2api/internal/pkg/clientidentity"
	"github.com/dofastted/claude2api/internal/pkg/tlsfingerprint"
)

func resolveClaudeCodeFamilyTLSProfile(account *Account, tlsService *TLSFingerprintProfileService, forceClaudeCLI bool) *tlsfingerprint.Profile {
	if snapshot := clientidentity.NewResolver().Resolve(account); snapshot != nil {
		if profile := tlsfingerprint.BuiltInProfileByName(strings.TrimSpace(snapshot.TLSProfileName)); profile != nil {
			return profile
		}
	}
	if forceClaudeCLI {
		return tlsfingerprint.BuiltInProfileByName(tlsfingerprint.ProfileNameClaudeCLIDefault)
	}
	if tlsService == nil {
		return nil
	}
	return tlsService.ResolveTLSProfile(account)
}

func (s *AccountTestService) resolveClaudeAccountTestTLSProfile(account *Account) *tlsfingerprint.Profile {
	return resolveClaudeCodeFamilyTLSProfile(account, s.tlsFPProfileService, account.IsAnthropicAPIKeyPassthroughEnabled())
}

func (s *AccountTestService) resolveClaudeAccountTestProbeTLSProfile(account *Account, profile *ClaudeEnvironmentProfile) *tlsfingerprint.Profile {
	if profile != nil && (account.IsAnthropicAPIKeyPassthroughEnabled() || accountHasClaudeEnvironmentProfileSource(account)) {
		if tlsProfile := resolveClaudeEnvironmentTLSProfile(profile); tlsProfile != nil {
			return tlsProfile
		}
	}
	return s.resolveClaudeAccountTestTLSProfile(account)
}
