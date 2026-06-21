//go:build !slim

package handler

import (
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/payment"
)

func wechatPaymentLegacyKey(cfg *config.Config) []byte {
	key, err := payment.ProvideEncryptionKey(cfg)
	if err != nil {
		return nil
	}
	return []byte(key)
}
