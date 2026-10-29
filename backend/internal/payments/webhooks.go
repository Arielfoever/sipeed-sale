package payments

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/stripe/stripe-go/v82"
)

// ErrWebhookNotConfigured 未配置 STRIPE_WEBHOOK_SECRET(stripe-cli
// `stripe listen` 会打印 whsec_...)。
var ErrWebhookNotConfigured = errors.New("payments: STRIPE_WEBHOOK_SECRET not configured")

// WebhookEvent 是验签通过后事件的最小视图。不用 stripe.Event 是因为
// stripe-cli 生成的测试事件里 `created` 是字符串,而 SDK 的
// Event.Created 是 int64,直接用 ConstructEvent 会在解析阶段失败
// (签名本身是通过的,纯粹是 SDK 结构体不兼容 CLI 的序列化格式)。
type WebhookEvent struct {
	ID   string          // 事件 ID
	Type string          // 如 checkout.session.completed
	Data json.RawMessage // data.object 原始 JSON(仅保留需要的字段)
}

// VerifyWebhook 使用官方 webhook 库验证签名(stripe-cli 转发的本地请求
// 或线上终端投递均适用),但不解析 SDK 的 Event 结构,避免 CLI 测试事件
// 的 `created` 字符串与 SDK int64 字段不兼容。
func (c *Client) VerifyWebhook(payload []byte, sigHeader string) (WebhookEvent, error) {
	if c.whSecret == "" {
		return WebhookEvent{}, ErrWebhookNotConfigured
	}
	// 只验签;容忍 stripe-cli / 后台 webhook 版本与 SDK 的差异
	if err := stripe.ValidatePayload(
		payload, sigHeader, c.whSecret,
		stripe.WithIgnoreAPIVersionMismatch(),
	); err != nil {
		return WebhookEvent{}, err
	}

	var envelope struct {
		ID   string `json:"id"`
		Type string `json:"type"`
		Data struct {
			Object json.RawMessage `json:"object"`
		} `json:"data"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return WebhookEvent{}, fmt.Errorf("parse webhook envelope: %w", err)
	}
	return WebhookEvent{ID: envelope.ID, Type: envelope.Type, Data: envelope.Data.Object}, nil
}

// CheckoutSession 是我们从 data.object 中实际需要的字段。
// 故意用 string / json.RawMessage 而不是 SDK 的强类型,避免 stripe-cli
// 测试事件(created 等字段为字符串)导致的解析失败。
type CheckoutSession struct {
	ID            string `json:"id"`
	PaymentStatus string `json:"payment_status"`
	// 真实投递里是字符串 "pi_...",SDK 展开形态是对象 {"id":"pi_..."},两者都兼容
	PaymentIntent json.RawMessage `json:"payment_intent"`
}

// PaymentIntentID 兼容 payment_intent 的两种序列化形态(字符串 / 对象)。
func (c *CheckoutSession) PaymentIntentID() string {
	if len(c.PaymentIntent) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(c.PaymentIntent, &s); err == nil {
		return s
	}
	var obj struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(c.PaymentIntent, &obj); err == nil {
		return obj.ID
	}
	return ""
}

// DecideMapping 把事件 → 目标状态:
//   - completed:payment_status == "paid" → PAID,否则等待异步事件
//   - async_payment_succeeded → PAID
//   - async_payment_failed    → FAILED
//   - expired                 → CANCELED
//
// 返回 (目标状态|"", paymentIntent|nil, 是否处理)。
func DecideMapping(evType string, cs *CheckoutSession) (string, *string, bool) {
	var piPtr *string
	if pi := cs.PaymentIntentID(); pi != "" {
		piPtr = &pi
	}
	switch evType {
	case string(stripe.EventTypeCheckoutSessionCompleted):
		// 异步付款方式(如银行扣款)completed 但未付,等 async 事件
		if cs.PaymentStatus == string(stripe.CheckoutSessionPaymentStatusPaid) {
			return "PAID", piPtr, true
		}
		return "", nil, false
	case string(stripe.EventTypeCheckoutSessionAsyncPaymentSucceeded):
		return "PAID", piPtr, true
	case string(stripe.EventTypeCheckoutSessionAsyncPaymentFailed):
		return "FAILED", piPtr, true
	case string(stripe.EventTypeCheckoutSessionExpired):
		return "CANCELED", nil, true
	default:
		return "", nil, false
	}
}
