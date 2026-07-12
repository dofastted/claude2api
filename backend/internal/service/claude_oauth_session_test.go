package service

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func newClaudeOAuthSessionResolverForTest(t *testing.T) *ClaudeOAuthSessionResolver {
	t.Helper()
	resolver, err := NewClaudeOAuthSessionResolver(ClaudeOAuthSessionKeys{
		CurrentSigningKeyID: "current",
		CurrentSigningKey:   bytes.Repeat([]byte{0x11}, 32),
		PreviousSigningKeys: map[string][]byte{"previous": bytes.Repeat([]byte{0x22}, 32)},
		BindingKey:          bytes.Repeat([]byte{0x33}, 32),
	})
	require.NoError(t, err)
	resolver.now = func() time.Time { return time.Unix(1_800_000_000, 0).UTC() }
	resolver.randRead = func(dst []byte) (int, error) {
		for i := range dst {
			dst[i] = byte(i + 1)
		}
		return len(dst), nil
	}
	return resolver
}

func TestClaudeOAuthSessionIssueAndResolvePreserveBinding(t *testing.T) {
	resolver := newClaudeOAuthSessionResolverForTest(t)
	namespace := ClaudeOAuthSessionNamespace{GroupID: 9, APIKeyID: 21}

	issued, err := resolver.Issue(namespace, "native-session")
	require.NoError(t, err)
	require.NotEmpty(t, issued.SignedToken)
	require.Equal(t, "current", issued.SigningKeyID)

	fromNative, err := resolver.Resolve(namespace, ClaudeOAuthSessionInput{NativeCandidates: []string{"native-session"}})
	require.NoError(t, err)
	fromSigned, err := resolver.Resolve(namespace, ClaudeOAuthSessionInput{SignedToken: issued.SignedToken})
	require.NoError(t, err)
	fromBoth, err := resolver.Resolve(namespace, ClaudeOAuthSessionInput{
		SignedToken:      issued.SignedToken,
		NativeCandidates: []string{"native-session", " native-session "},
	})
	require.NoError(t, err)

	require.Equal(t, fromNative.BindingHash, fromSigned.BindingHash)
	require.Equal(t, fromSigned.BindingHash, fromBoth.BindingHash)
	require.Equal(t, "native", fromNative.Source)
	require.Equal(t, "signed", fromSigned.Source)
}

func TestClaudeOAuthSessionRejectsConflictsTamperingAndNamespaceReplay(t *testing.T) {
	resolver := newClaudeOAuthSessionResolverForTest(t)
	namespace := ClaudeOAuthSessionNamespace{GroupID: 9, APIKeyID: 21}
	issued, err := resolver.Issue(namespace, "native-session")
	require.NoError(t, err)

	_, err = resolver.Resolve(namespace, ClaudeOAuthSessionInput{
		SignedToken:      issued.SignedToken,
		NativeCandidates: []string{"different-native"},
	})
	require.ErrorIs(t, err, ErrClaudeOAuthSessionConflict)

	_, err = resolver.Resolve(namespace, ClaudeOAuthSessionInput{NativeCandidates: []string{"one", "two"}})
	require.ErrorIs(t, err, ErrClaudeOAuthSessionConflict)

	tampered := issued.SignedToken[:len(issued.SignedToken)-1] + "A"
	_, err = resolver.Resolve(namespace, ClaudeOAuthSessionInput{SignedToken: tampered})
	require.ErrorIs(t, err, ErrClaudeOAuthSessionInvalid)

	_, err = resolver.Resolve(ClaudeOAuthSessionNamespace{GroupID: 9, APIKeyID: 22}, ClaudeOAuthSessionInput{SignedToken: issued.SignedToken})
	require.ErrorIs(t, err, ErrClaudeOAuthSessionInvalid)
}

func TestClaudeOAuthSessionRejectsMissingAndExpired(t *testing.T) {
	resolver := newClaudeOAuthSessionResolverForTest(t)
	namespace := ClaudeOAuthSessionNamespace{GroupID: 9, APIKeyID: 21}

	_, err := resolver.Resolve(namespace, ClaudeOAuthSessionInput{})
	require.ErrorIs(t, err, ErrClaudeOAuthSessionMissing)

	issued, err := resolver.Issue(namespace, "native-session")
	require.NoError(t, err)
	resolver.now = func() time.Time { return time.Unix(1_800_000_000+ClaudeOAuthSessionTTLSeconds, 0).UTC() }
	_, err = resolver.Resolve(namespace, ClaudeOAuthSessionInput{SignedToken: issued.SignedToken})
	require.ErrorIs(t, err, ErrClaudeOAuthSessionInvalid)
}

func TestClaudeOAuthSessionPreviousSigningKeyValidDuringOverlap(t *testing.T) {
	resolver := newClaudeOAuthSessionResolverForTest(t)
	namespace := ClaudeOAuthSessionNamespace{GroupID: 9, APIKeyID: 21}
	now := resolver.now().UTC()
	payload := claudeOAuthSessionTokenPayload{
		Version:       claudeOAuthSessionTokenVersion,
		KeyID:         "previous",
		NamespaceHash: resolver.namespaceHash(namespace),
		Subject:       "previous-subject",
		IssuedAt:      now.Unix(),
		ExpiresAt:     now.Add(time.Hour).Unix(),
	}
	token, err := resolver.signPayload(payload, resolver.keys.PreviousSigningKeys["previous"])
	require.NoError(t, err)

	resolved, err := resolver.Resolve(namespace, ClaudeOAuthSessionInput{SignedToken: token})
	require.NoError(t, err)
	require.Equal(t, "previous", resolved.SigningKeyID)
	require.False(t, strings.Contains(resolved.BindingHash, "previous-subject"))
}
