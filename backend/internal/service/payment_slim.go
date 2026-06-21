//go:build slim

package service

import (
	"context"

	dbent "github.com/Wei-Shaw/sub2api/ent"
)

type PaymentConfig struct {
	Enabled                   bool     `json:"enabled"`
	MinAmount                 float64  `json:"min_amount"`
	MaxAmount                 float64  `json:"max_amount"`
	DailyLimit                float64  `json:"daily_limit"`
	OrderTimeoutMin           int      `json:"order_timeout_minutes"`
	MaxPendingOrders          int      `json:"max_pending_orders"`
	EnabledTypes              []string `json:"enabled_payment_types"`
	BalanceDisabled           bool     `json:"balance_disabled"`
	BalanceRechargeMultiplier float64  `json:"balance_recharge_multiplier"`
	RechargeFeeRate           float64  `json:"recharge_fee_rate"`
	LoadBalanceStrategy       string   `json:"load_balance_strategy"`
	ProductNamePrefix         string   `json:"product_name_prefix"`
	ProductNameSuffix         string   `json:"product_name_suffix"`
	HelpImageURL              string   `json:"help_image_url"`
	HelpText                  string   `json:"help_text"`
	StripePublishableKey      string   `json:"stripe_publishable_key,omitempty"`

	CancelRateLimitEnabled bool   `json:"cancel_rate_limit_enabled"`
	CancelRateLimitMax     int    `json:"cancel_rate_limit_max"`
	CancelRateLimitWindow  int    `json:"cancel_rate_limit_window"`
	CancelRateLimitUnit    string `json:"cancel_rate_limit_unit"`
	CancelRateLimitMode    string `json:"cancel_rate_limit_window_mode"`

	AlipayForceQRCode bool `json:"alipay_force_qrcode"`
}

type UpdatePaymentConfigRequest struct {
	Enabled                   *bool    `json:"enabled"`
	MinAmount                 *float64 `json:"min_amount"`
	MaxAmount                 *float64 `json:"max_amount"`
	DailyLimit                *float64 `json:"daily_limit"`
	OrderTimeoutMin           *int     `json:"order_timeout_minutes"`
	MaxPendingOrders          *int     `json:"max_pending_orders"`
	EnabledTypes              []string `json:"enabled_payment_types"`
	BalanceDisabled           *bool    `json:"balance_disabled"`
	BalanceRechargeMultiplier *float64 `json:"balance_recharge_multiplier"`
	RechargeFeeRate           *float64 `json:"recharge_fee_rate"`
	LoadBalanceStrategy       *string  `json:"load_balance_strategy"`
	ProductNamePrefix         *string  `json:"product_name_prefix"`
	ProductNameSuffix         *string  `json:"product_name_suffix"`
	HelpImageURL              *string  `json:"help_image_url"`
	HelpText                  *string  `json:"help_text"`

	CancelRateLimitEnabled *bool   `json:"cancel_rate_limit_enabled"`
	CancelRateLimitMax     *int    `json:"cancel_rate_limit_max"`
	CancelRateLimitWindow  *int    `json:"cancel_rate_limit_window"`
	CancelRateLimitUnit    *string `json:"cancel_rate_limit_unit"`
	CancelRateLimitMode    *string `json:"cancel_rate_limit_window_mode"`

	AlipayForceQRCode *bool `json:"alipay_force_qrcode"`

	VisibleMethodAlipaySource  *string `json:"payment_visible_method_alipay_source"`
	VisibleMethodWxpaySource   *string `json:"payment_visible_method_wxpay_source"`
	VisibleMethodAlipayEnabled *bool   `json:"payment_visible_method_alipay_enabled"`
	VisibleMethodWxpayEnabled  *bool   `json:"payment_visible_method_wxpay_enabled"`
}

type PaymentConfigService struct{}

func (s *PaymentConfigService) GetPaymentConfig(context.Context) (*PaymentConfig, error) {
	return &PaymentConfig{}, nil
}

func (s *PaymentConfigService) UpdatePaymentConfig(context.Context, UpdatePaymentConfigRequest) error {
	return nil
}

type PaymentService struct{}

func (s *PaymentService) RefreshProviders(context.Context) {}

type OrderListParams struct {
	Page        int
	PageSize    int
	Status      string
	OrderType   string
	PaymentType string
	Keyword     string
}

type DashboardStats struct {
	TodayAmount   float64 `json:"today_amount"`
	TotalAmount   float64 `json:"total_amount"`
	TodayCount    int     `json:"today_count"`
	TotalCount    int     `json:"total_count"`
	AvgAmount     float64 `json:"avg_amount"`
	PendingOrders int     `json:"pending_orders"`

	DailySeries    []DailyStats        `json:"daily_series"`
	PaymentMethods []PaymentMethodStat `json:"payment_methods"`
	TopUsers       []TopUserStat       `json:"top_users"`
}

type DailyStats struct {
	Date   string  `json:"date"`
	Amount float64 `json:"amount"`
	Count  int     `json:"count"`
}

type PaymentMethodStat struct {
	Type   string  `json:"type"`
	Amount float64 `json:"amount"`
	Count  int     `json:"count"`
}

type TopUserStat struct {
	UserID int64   `json:"user_id"`
	Email  string  `json:"email"`
	Amount float64 `json:"amount"`
}

func (s *PaymentService) GetDashboardStats(context.Context, int) (*DashboardStats, error) {
	return &DashboardStats{}, nil
}

func (s *PaymentService) AdminListOrders(context.Context, int64, OrderListParams) ([]*dbent.PaymentOrder, int, error) {
	return nil, 0, nil
}

func (s *PaymentService) GetOrderByID(context.Context, int64) (*dbent.PaymentOrder, error) {
	return nil, nil
}
