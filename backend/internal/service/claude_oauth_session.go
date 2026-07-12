package service

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	ClaudeOAuthSignedSessionHeader = "X-CLIProxy-Session-ID"
	ClaudeOAuthNativeSessionHeader = "X-Claude-Code-Session-Id"
	claudeOAuthSessionTokenVersion = "v1"
)

var (
	ErrClaudeOAuthSessionMissing  = errors.New("invalid_session_id")
	ErrClaudeOAuthSessionConflict = errors.New("conflicting_session_id")
	ErrClaudeOAuthSessionInvalid  = errors.New("invalid signed session token")
)

type ClaudeOAuthSessionNamespace struct {
	GroupID  int64
	APIKeyID int64
}

type ClaudeOAuthSessionInput struct {
	SignedToken      string
	NativeCandidates []string
}

type ClaudeOAuthResolvedSession struct {
	BindingHash  string
	SubjectHash  string
	NativeHash   string
	Source       string
	SignedToken  string
	TokenExpires time.Time
	SigningKeyID string
}

type ClaudeOAuthSessionKeys struct {
	CurrentSigningKeyID string
	CurrentSigningKey   []byte
	PreviousSigningKeys map[string][]byte
	BindingKey          []byte
}

type ClaudeOAuthSessionResolver struct {
	keys     ClaudeOAuthSessionKeys
	now      func() time.Time
	randRead func([]byte) (int, error)
}

type claudeOAuthSessionTokenPayload struct {
	Version       string `json:"v"`
	KeyID         string `json:"kid"`
	NamespaceHash string `json:"ns"`
	Subject       string `json:"sub"`
	NativeHash    string `json:"nh,omitempty"`
	IssuedAt      int64  `json:"iat"`
	ExpiresAt     int64  `json:"exp"`
}

func NewClaudeOAuthSessionResolver(keys ClaudeOAuthSessionKeys) (*ClaudeOAuthSessionResolver, error) {
	keys.CurrentSigningKeyID = strings.TrimSpace(keys.CurrentSigningKeyID)
	if keys.CurrentSigningKeyID == "" || len(keys.CurrentSigningKey) < 32 || len(keys.BindingKey) < 32 {
		return nil, fmt.Errorf("%w: current signing key id, 32-byte signing key and 32-byte binding key are required", ErrClaudeOAuthSessionInvalid)
	}
	return &ClaudeOAuthSessionResolver{keys: keys, now: time.Now, randRead: rand.Read}, nil
}

func (r *ClaudeOAuthSessionResolver) Resolve(namespace ClaudeOAuthSessionNamespace, input ClaudeOAuthSessionInput) (*ClaudeOAuthResolvedSession, error) {
	if err := validateClaudeOAuthSessionNamespace(namespace); err != nil {
		return nil, err
	}
	native, err := uniqueNativeSession(input.NativeCandidates)
	if err != nil {
		return nil, err
	}
	var nativeHash string
	if native != "" {
		nativeHash = r.nativeHash(namespace, native)
	}

	signedToken := strings.TrimSpace(input.SignedToken)
	if signedToken == "" {
		if nativeHash == "" {
			return nil, ErrClaudeOAuthSessionMissing
		}
		return r.resolved(namespace, nativeHash, nativeHash, "native", "", time.Time{}, ""), nil
	}

	payload, err := r.verifyToken(namespace, signedToken)
	if err != nil {
		return nil, err
	}
	if nativeHash != "" && payload.NativeHash != nativeHash {
		return nil, ErrClaudeOAuthSessionConflict
	}
	subjectHash := payload.NativeHash
	if subjectHash == "" {
		subjectHash = payload.Subject
	}
	return r.resolved(namespace, subjectHash, payload.NativeHash, "signed", signedToken, time.Unix(payload.ExpiresAt, 0).UTC(), payload.KeyID), nil
}

func (r *ClaudeOAuthSessionResolver) Issue(namespace ClaudeOAuthSessionNamespace, nativeSession string) (*ClaudeOAuthResolvedSession, error) {
	if err := validateClaudeOAuthSessionNamespace(namespace); err != nil {
		return nil, err
	}
	nativeSession = strings.TrimSpace(nativeSession)
	if nativeSession == "" {
		return nil, ErrClaudeOAuthSessionMissing
	}
	nonce := make([]byte, 16)
	if _, err := r.randRead(nonce); err != nil {
		return nil, fmt.Errorf("generate signed session subject: %w", err)
	}
	now := r.now().UTC()
	payload := claudeOAuthSessionTokenPayload{
		Version:       claudeOAuthSessionTokenVersion,
		KeyID:         r.keys.CurrentSigningKeyID,
		NamespaceHash: r.namespaceHash(namespace),
		Subject:       base64.RawURLEncoding.EncodeToString(nonce),
		NativeHash:    r.nativeHash(namespace, nativeSession),
		IssuedAt:      now.Unix(),
		ExpiresAt:     now.Add(time.Duration(ClaudeOAuthSessionTTLSeconds) * time.Second).Unix(),
	}
	token, err := r.signPayload(payload, r.keys.CurrentSigningKey)
	if err != nil {
		return nil, err
	}
	return r.resolved(namespace, payload.NativeHash, payload.NativeHash, "signed", token, time.Unix(payload.ExpiresAt, 0).UTC(), payload.KeyID), nil
}

func (r *ClaudeOAuthSessionResolver) verifyToken(namespace ClaudeOAuthSessionNamespace, token string) (*claudeOAuthSessionTokenPayload, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 4 || parts[0] != claudeOAuthSessionTokenVersion {
		return nil, ErrClaudeOAuthSessionInvalid
	}
	keyID := strings.TrimSpace(parts[1])
	key := r.signingKey(keyID)
	if len(key) == 0 {
		return nil, ErrClaudeOAuthSessionInvalid
	}
	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, ErrClaudeOAuthSessionInvalid
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[3])
	if err != nil {
		return nil, ErrClaudeOAuthSessionInvalid
	}
	if !hmac.Equal(signature, hmacSHA256(key, []byte(parts[0]+"."+parts[1]+"."+parts[2]))) {
		return nil, ErrClaudeOAuthSessionInvalid
	}
	var payload claudeOAuthSessionTokenPayload
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return nil, ErrClaudeOAuthSessionInvalid
	}
	if payload.Version != claudeOAuthSessionTokenVersion || payload.KeyID != keyID || payload.NamespaceHash != r.namespaceHash(namespace) || payload.Subject == "" {
		return nil, ErrClaudeOAuthSessionInvalid
	}
	now := r.now().Unix()
	if payload.IssuedAt <= 0 || payload.ExpiresAt <= payload.IssuedAt || now < payload.IssuedAt-30 || now >= payload.ExpiresAt {
		return nil, ErrClaudeOAuthSessionInvalid
	}
	return &payload, nil
}

func (r *ClaudeOAuthSessionResolver) signPayload(payload claudeOAuthSessionTokenPayload, key []byte) (string, error) {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode signed session token: %w", err)
	}
	encoded := base64.RawURLEncoding.EncodeToString(payloadBytes)
	prefix := claudeOAuthSessionTokenVersion + "." + payload.KeyID + "." + encoded
	signature := base64.RawURLEncoding.EncodeToString(hmacSHA256(key, []byte(prefix)))
	return prefix + "." + signature, nil
}

func (r *ClaudeOAuthSessionResolver) signingKey(keyID string) []byte {
	if keyID == r.keys.CurrentSigningKeyID {
		return r.keys.CurrentSigningKey
	}
	return r.keys.PreviousSigningKeys[keyID]
}

func (r *ClaudeOAuthSessionResolver) namespaceHash(namespace ClaudeOAuthSessionNamespace) string {
	return domainHMAC(r.keys.BindingKey, "claude-oauth-namespace-v1", namespaceBytes(namespace))
}

func (r *ClaudeOAuthSessionResolver) nativeHash(namespace ClaudeOAuthSessionNamespace, native string) string {
	return domainHMAC(r.keys.BindingKey, "claude-oauth-native-v1", append(namespaceBytes(namespace), []byte(native)...))
}

func (r *ClaudeOAuthSessionResolver) resolved(namespace ClaudeOAuthSessionNamespace, subjectHash, nativeHash, source, token string, expires time.Time, keyID string) *ClaudeOAuthResolvedSession {
	bindingMaterial := append(namespaceBytes(namespace), []byte(subjectHash)...)
	return &ClaudeOAuthResolvedSession{
		BindingHash:  domainHMAC(r.keys.BindingKey, "claude-oauth-binding-v1", bindingMaterial),
		SubjectHash:  subjectHash,
		NativeHash:   nativeHash,
		Source:       source,
		SignedToken:  token,
		TokenExpires: expires,
		SigningKeyID: keyID,
	}
}

func validateClaudeOAuthSessionNamespace(namespace ClaudeOAuthSessionNamespace) error {
	if namespace.GroupID <= 0 || namespace.APIKeyID <= 0 {
		return fmt.Errorf("%w: authenticated group and api key are required", ErrClaudeOAuthSessionInvalid)
	}
	return nil
}

func uniqueNativeSession(candidates []string) (string, error) {
	var native string
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if native == "" {
			native = candidate
			continue
		}
		if native != candidate {
			return "", ErrClaudeOAuthSessionConflict
		}
	}
	return native, nil
}

func namespaceBytes(namespace ClaudeOAuthSessionNamespace) []byte {
	out := make([]byte, 16)
	binary.BigEndian.PutUint64(out[:8], uint64(namespace.GroupID))
	binary.BigEndian.PutUint64(out[8:], uint64(namespace.APIKeyID))
	return out
}

func domainHMAC(key []byte, domain string, material []byte) string {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(domain))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write(material)
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func hmacSHA256(key, material []byte) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(material)
	return mac.Sum(nil)
}
