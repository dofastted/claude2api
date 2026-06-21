//go:build slim

package handler

import "github.com/Wei-Shaw/sub2api/internal/config"

func wechatPaymentLegacyKey(*config.Config) []byte {
	return nil
}
