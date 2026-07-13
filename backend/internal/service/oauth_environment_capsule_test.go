package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEnsureEnvironmentCapsulesForThreeOAuthFamilies(t *testing.T) {
	cases := []struct {
		name     string
		platform string
		family   string
	}{
		{"claude", PlatformAnthropic, EnvironmentCapsuleFamilyClaude},
		{"codex", PlatformOpenAI, EnvironmentCapsuleFamilyCodex},
		{"grok", PlatformGrok, EnvironmentCapsuleFamilyGrok},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			account := &Account{ID: 100 + int64(len(tc.name)), Platform: tc.platform, Type: AccountTypeOAuth, Status: StatusActive, Extra: map[string]any{}}
			_, err := EnsureEnvironmentCapsules(account)
			require.NoError(t, err)
			summary, err := EnvironmentCapsuleSummaryFromAccount(account)
			require.NoError(t, err)
			require.Equal(t, tc.family, summary.Family)
			require.Equal(t, account.ID, summary.CredentialID)
			require.Len(t, summary.Slots, 3)
			require.Equal(t, "windows", summary.Slots[0].Environment)
			require.Equal(t, "macos", summary.Slots[1].Environment)
			require.Equal(t, "linux", summary.Slots[2].Environment)
			// idempotent
			_, err = EnsureEnvironmentCapsules(account)
			require.NoError(t, err)
			again, err := EnvironmentCapsuleSummaryFromAccount(account)
			require.NoError(t, err)
			require.Equal(t, summary.Digest, again.Digest)
		})
	}
}

func TestEnsureEnvironmentCapsulesRejectsNonOAuth(t *testing.T) {
	_, err := EnsureEnvironmentCapsules(&Account{ID: 1, Platform: PlatformAnthropic, Type: AccountTypeAPIKey})
	require.Error(t, err)
	_, err = EnsureEnvironmentCapsules(&Account{ID: 1, Platform: PlatformGemini, Type: AccountTypeOAuth})
	require.Error(t, err)
}

func TestRejectEnvironmentCapsuleIdentityEdit(t *testing.T) {
	require.Error(t, RejectEnvironmentCapsuleIdentityEdit(&Account{Platform: PlatformAnthropic, Type: AccountTypeOAuth}))
	require.Error(t, RejectEnvironmentCapsuleIdentityEdit(&Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth}))
	require.Error(t, RejectEnvironmentCapsuleIdentityEdit(&Account{Platform: PlatformGrok, Type: AccountTypeOAuth}))
	require.NoError(t, RejectEnvironmentCapsuleIdentityEdit(&Account{Platform: PlatformAnthropic, Type: AccountTypeSetupToken}))
	require.NoError(t, RejectEnvironmentCapsuleIdentityEdit(&Account{Platform: PlatformAnthropic, Type: AccountTypeAPIKey}))
}

func TestPublishClaudeOAuthCapsulesIncrementsVersion(t *testing.T) {
	account := &Account{ID: 55, Platform: PlatformAnthropic, Type: AccountTypeOAuth, Extra: map[string]any{}}
	first, err := EnsureClaudeOAuthCapsules(account)
	require.NoError(t, err)
	require.Equal(t, int64(1), first.Version)
	second, err := PublishClaudeOAuthCapsules(account, "2.1.5", "UTC")
	require.NoError(t, err)
	require.Equal(t, int64(2), second.Version)
	require.NotEqual(t, first.Digest, second.Digest)
}
