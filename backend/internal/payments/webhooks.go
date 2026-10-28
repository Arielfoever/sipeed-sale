package payments

import (
	"errors"

	"github.com/stripe/stripe-go/v82"
)

// ErrWebhookNotConfigured 未配置 STRIPE_WEBHOOK_SECRET(stripe-cli
// `stripe listen` 会打印 whsec_...)。
var ErrWebhookNotConfigured = errors.New("payments: STRIPE_WEBHOOK_SECRET not configured")

// VerifyWebhook 使用官方 webhook 库验证签名并解析事件
// (stripe-cli 转发的本地请求或线上终端投递均适用)。
func (c *Client) VerifyWebhook(payload []byte, sigHeader string) (stripe.Event, error) {
	if c.whSecret == "" {
		return stripe.Event{}, ErrWebhookNotConfigured
	}
	// 官方签名校验;容忍 stripe-cli / 后台 webhook 版本与 SDK 的差异
	return stripe.ConstructEvent(
		payload, sigHeader, c.whSecret,
		stripe.WithIgnoreAPIVersionMismatch(),
	)
}

// Outcome 描述一次事件处理结果。
type Outcome int

const (
	OutcomeIgnored    Outcome = iota // 非本系统关心的事件
	OutcomeApplied                   // 状态已推进(首次)
	OutcomeIdempotent                // 已处理过(重复投递命中 0 行)
)

// ProcessCheckoutEvent 按 spec 约定处理 checkout.session.* 事件,
// 订单状态仅由 webhook 推进(浏览器回跳不改状态)。
// 事件中的 CheckoutSession 由调用方解码(stripe.CheckoutSession)后传入。
type Processor interface {
	MarkOrderStatusBySession(sessionID, newStatus string, paymentIntent *string) (int64, error)
}

// DecideMapping 讲清楚事件 → 目标状态:
//   - completed:sessions.payment_status == "paid" → PAID,否则等待异步事件
//   - async_payment_succeeded → PAID
//   - async_payment_failed    → FAILED
//   - expired                 → CANCELED
//
// 返回 (目标状态|"", paymentIntent|nil, 是否处理)。
func DecideMapping(evType stripe.EventType, cs *stripe.CheckoutSession) (string, *string, bool) {
	var piPtr *string
	if cs.PaymentIntent != nil {
		pi := cs.PaymentIntent.ID
		if pi != "" {
			piPtr = &pi
		}
	}
	switch evType {
	case stripe.EventTypeCheckoutSessionCompleted:
		// 异步付款方式(如银行扣款)completed 但未付,等 async 事件
		if cs.PaymentStatus == stripe.CheckoutSessionPaymentStatusPaid {
			return "PAID", piPtr, true
		}
		return "", nil, false
	case stripe.EventTypeCheckoutSessionAsyncPaymentSucceeded:
		return "PAID", piPtr, true
	case stripe.EventTypeCheckoutSessionAsyncPaymentFailed:
		return "FAILED", piPtr, true
	case stripe.EventTypeCheckoutSessionExpired:
		return "CANCELED", nil, true
	default:
		return "", nil, false
	}
}
