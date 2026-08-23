package service

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	"github.com/stretchr/testify/require"
)

func TestDefaultCLIIdentity_MatchesOfficial241(t *testing.T) {
	require.Equal(t, "2.1.241", claude.CLICurrentVersion)
	require.Equal(t, "claude-cli/2.1.241 (external, sdk-cli)", claude.DefaultCLIUserAgent())
	require.Equal(t, claude.DefaultCLIUserAgent(), claude.DefaultHeaders["User-Agent"])
	require.Equal(t, "cli", claude.DefaultHeaders["X-App"])
	require.Equal(t, "0.112.1", claude.DefaultHeaders["X-Stainless-Package-Version"])
	require.Equal(t, "v26.3.0", claude.DefaultHeaders["X-Stainless-Runtime-Version"])
}

func TestExtractFirstUserText_SkipsLeadingSystemReminder(t *testing.T) {
	visible := "搜索apple股票今日股价,以及2026年iPhone有什么更新."
	body := []byte(`{
		"messages":[
			{"role":"user","content":[
				{"type":"text","text":"<system-reminder>\nAs you answer the user's questions, you can use the following context:\n</system-reminder>"},
				{"type":"text","text":"` + visible + `"}
			]}
		]
	}`)
	require.Equal(t, visible, extractFirstUserText(body))
}

func TestExtractFirstUserText_FallsBackToReminderOnly(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":[{"type":"text","text":"<system-reminder>\nonly reminder\n</system-reminder>"}]}]}`)
	require.Equal(t, "<system-reminder>\nonly reminder\n</system-reminder>", extractFirstUserText(body))
}

func TestComputeClaudeCodeFingerprint_Official241VisiblePrompt(t *testing.T) {
	visible := "搜索apple股票今日股价,以及2026年iPhone有什么更新."
	body := []byte(`{
		"messages":[
			{"role":"user","content":[
				{"type":"text","text":"<system-reminder>\nAs you answer the user's questions, you can use the following context:\n</system-reminder>"},
				{"type":"text","text":"` + visible + `"}
			]}
		]
	}`)
	require.Equal(t, "134", computeClaudeCodeFingerprint(body, "2.1.241"))
	require.NotEqual(t, "134", computeClaudeCodeFingerprint([]byte(`{
		"messages":[{"role":"user","content":[{"type":"text","text":"<system-reminder>\nAs you answer the user's questions, you can use the following context:\n</system-reminder>"}]}]
	}`), "2.1.241"))
}

func TestBuildBillingAttributionText_Official241Shape(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":"hello world from client"}]}`)
	got, err := buildBillingAttributionText(body, claude.CLICurrentVersion)
	require.NoError(t, err)
	require.Contains(t, got, "x-anthropic-billing-header:")
	require.Contains(t, got, "cc_version="+claude.CLICurrentVersion+".")
	require.Contains(t, got, "cc_entrypoint=sdk-cli;")
	require.Contains(t, got, "cc_prompt_id=")
	require.NotContains(t, got, "cch=")
	require.NotContains(t, got, "cc_prev_req=")

	again, err := buildBillingAttributionText(body, claude.CLICurrentVersion)
	require.NoError(t, err)
	require.Equal(t, got, again, "cc_prompt_id must be stable for the same first user text")
}
