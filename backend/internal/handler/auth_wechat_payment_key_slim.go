//go:build slim

package handler

import "github.com/dofastted/claude2api/internal/config"

func wechatPaymentLegacyKey(*config.Config) []byte {
	return nil
}
