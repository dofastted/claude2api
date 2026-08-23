package service

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/tidwall/gjson"
)

// fingerprintSalt 是计算 cc_version 后缀指纹的盐值。
//
// 来源：与 Parrot src/transform/cc_mimicry.py 的 FINGERPRINT_SALT 完全一致；
// 这是真实 Claude Code CLI 抓包推导出的常量，改动会导致 fp 与 CLI 不一致，
// 进一步触发 Anthropic 的第三方检测。
const fingerprintSalt = "59cf53e54c78"

// computeClaudeCodeFingerprint 复刻真实 Claude Code CLI 的 cc_version 指纹算法：
//
//  1. 取 messages 中第一条 role=user 的可见纯文本（跳过 leading <system-reminder>）
//  2. 取该文本的第 4、7、20 字符（不足以 '0' 补齐）
//  3. SHA256(SALT + chars + cc_version) 取 hex 前 3 字符
//
// 算法来自 Parrot src/transform/cc_mimicry.py:compute_fingerprint。
// 官方 CLI 2.1.241 主对话对可见用户句取样，而不是首个 system-reminder 块。
func computeClaudeCodeFingerprint(body []byte, version string) string {
	firstText := extractFirstUserText(body)
	runes := []rune(firstText)
	indices := []int{4, 7, 20}
	sampled := make([]rune, 0, 3)
	for _, i := range indices {
		if i < len(runes) {
			sampled = append(sampled, runes[i])
		} else {
			sampled = append(sampled, '0')
		}
	}
	sum := sha256.Sum256([]byte(fingerprintSalt + string(sampled) + version))
	return hex.EncodeToString(sum[:])[:3]
}

func isClaudeSystemReminderText(text string) bool {
	return strings.HasPrefix(strings.TrimSpace(text), "<system-reminder>")
}

// extractFirstUserText 提取 messages 中第一条 user 消息的可见 text。
// 兼容 string 和 []block 两种 content 格式。
// 官方 CLI 2.1.241 会把 <system-reminder> 放在同一条 user 的第一块，指纹取其后的可见句。
func extractFirstUserText(body []byte) string {
	messages := gjson.GetBytes(body, "messages")
	if !messages.IsArray() {
		return ""
	}
	first := ""
	messages.ForEach(func(_, msg gjson.Result) bool {
		if msg.Get("role").String() != "user" {
			return true
		}
		content := msg.Get("content")
		if content.Type == gjson.String {
			first = content.String()
			return false
		}
		if content.IsArray() {
			var fallback string
			content.ForEach(func(_, block gjson.Result) bool {
				if block.Get("type").String() != "" && block.Get("type").String() != "text" {
					return true
				}
				text := block.Get("text").String()
				if text == "" {
					return true
				}
				if fallback == "" {
					fallback = text
				}
				if isClaudeSystemReminderText(text) {
					return true
				}
				first = text
				return false
			})
			if first == "" {
				first = fallback
			}
			return false
		}
		return false
	})
	return first
}

// buildBillingAttributionText 构造 system 数组的 billing attribution 文本。
//
// 形态对齐官方 CLI 2.1.241 主对话：
//
//	x-anthropic-billing-header: cc_version=2.1.241.{fp}; cc_entrypoint=sdk-cli; cc_prompt_id={uuid};
//
// cch 旧 xxHash 实现未通过 2.1.241 抓包验算，禁止写假 cch=00000。
// cc_prev_req 依赖上一轮上游 request id，本函数无会话状态，不伪造。
//
// 此 block 不带 cache_control（与真实 CLI 一致；cache breakpoint 由后续扩写块承担）。
func buildBillingAttributionText(body []byte, cliVersion string) (string, error) {
	if cliVersion == "" {
		return "", fmt.Errorf("cliVersion required")
	}
	fp := computeClaudeCodeFingerprint(body, cliVersion)
	promptID := generateSessionUUID("cc_prompt_id::" + extractFirstUserText(body))
	return fmt.Sprintf(
		"x-anthropic-billing-header: cc_version=%s.%s; cc_entrypoint=sdk-cli; cc_prompt_id=%s;",
		cliVersion, fp, promptID,
	), nil
}
