//go:build slim

package service

import "strings"

const (
	SettingPaymentEnabled                    = "payment_enabled"
	SettingPaymentVisibleMethodAlipaySource  = "payment_visible_method_alipay_source"
	SettingPaymentVisibleMethodWxpaySource   = "payment_visible_method_wxpay_source"
	SettingPaymentVisibleMethodAlipayEnabled = "payment_visible_method_alipay_enabled"
	SettingPaymentVisibleMethodWxpayEnabled  = "payment_visible_method_wxpay_enabled"

	VisibleMethodSourceOfficialAlipay = "official_alipay"
	VisibleMethodSourceEasyPayAlipay  = "easypay_alipay"
	VisibleMethodSourceOfficialWechat = "official_wxpay"
	VisibleMethodSourceEasyPayWechat  = "easypay_wxpay"
)

func NormalizeVisibleMethod(method string) string {
	normalized := strings.ToLower(strings.TrimSpace(method))
	switch normalized {
	case "alipay", "alipay_direct":
		return "alipay"
	case "wxpay", "wxpay_direct", "wechat":
		return "wxpay"
	default:
		return normalized
	}
}

func NormalizeVisibleMethodSource(method, source string) string {
	switch NormalizeVisibleMethod(method) {
	case "alipay":
		switch strings.ToLower(strings.TrimSpace(source)) {
		case VisibleMethodSourceOfficialAlipay, "alipay", "alipay_direct", "official":
			return VisibleMethodSourceOfficialAlipay
		case VisibleMethodSourceEasyPayAlipay, "easypay":
			return VisibleMethodSourceEasyPayAlipay
		}
	case "wxpay":
		switch strings.ToLower(strings.TrimSpace(source)) {
		case VisibleMethodSourceOfficialWechat, "wxpay", "wxpay_direct", "wechat", "official":
			return VisibleMethodSourceOfficialWechat
		case VisibleMethodSourceEasyPayWechat, "easypay":
			return VisibleMethodSourceEasyPayWechat
		}
	}
	return ""
}
