package payments

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/stripe/stripe-go/v82"
)

// ErrWebhookNotConfigured 未配置 STRIPE_WEBHOOK_SECRET(stripe-cli
// `stripe listen` 会打印 whsec_...)。
var ErrWebhookNotConfigured = errors.New("payments: STRIPE_WEBHOOK_SECRET not configured")

// Stripe 现在默认投递 thin event(`object: v2.core.event`),其中没有
// `data.object`,只有 `related_object` 指针 {id,type,url};处理时必须按
// 官方要求用 Stripe API 拉取关联对象的完整内容(见 FetchCheckoutSession)。
type WebhookEvent struct {
	ID   string          // 事件 ID
	Type string          // 如 checkout.session.completed(已去版本前缀)
	Data json.RawMessage // data.object 原始 JSON(snapshot event 才有;thin 为空)
	// RelatedObject 是 thin event 的关联对象指针(id/type/url),snapshot 为空
	RelatedObject *RelatedObject
}

// RelatedObject 是 thin event 里对关联对象的引用(需再 fetch 完整内容)。
type RelatedObject struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	URL  string `json:"url"`
}

// VerifyWebhook 使用官方 webhook 库验证签名
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
		RelatedObject *RelatedObject `json:"related_object"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return WebhookEvent{}, fmt.Errorf("parse webhook envelope: %w", err)
	}
	return WebhookEvent{
		ID:            envelope.ID,
		Type:          NormalizeEventType(envelope.Type),
		Data:          envelope.Data.Object,
		RelatedObject: envelope.RelatedObject,
	}, nil
}

// CheckoutSession 是我们从 CheckoutSession 对象中实际需要的字段。

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

// FetchCheckoutSession 按 Stripe 官方 thin event 要求,从 API 拉取与事件
// 关联的 CheckoutSession 完整对象(thin event 的 related_object 只是指针,
// 没有 data.object;snapshot event 也可统一走此权威路径)。
// 返回我们关心的字段;错误可直接用 IsNotFound 判断对象是否已不存在。
func (c *Client) FetchCheckoutSession(ctx context.Context, sessionID string) (*CheckoutSession, error) {
	if c.sc == nil {
		return nil, ErrNotConfigured
	}
	sess, err := c.sc.V1CheckoutSessions.Retrieve(ctx, sessionID, nil)
	if err != nil {
		return nil, err
	}
	cs := &CheckoutSession{ID: sess.ID, PaymentStatus: string(sess.PaymentStatus)}
	if sess.PaymentIntent != nil && sess.PaymentIntent.ID != "" {
		// 统一存成字符串形态("pi_..."),PaymentIntentID() 两种都能解
		b, _ := json.Marshal(sess.PaymentIntent.ID)
		cs.PaymentIntent = b
	}
	return cs, nil
}

// IsNotFound 判断 Stripe API 错误是否为对象不存在(404)。
func IsNotFound(err error) bool {
	var se *stripe.Error
	if errors.As(err, &se) {
		return se.HTTPStatusCode == http.StatusNotFound
	}
	return false
}

func NormalizeEventType(evType string) string {
	seg, _, ok := strings.Cut(evType, ".")
	if !ok {
		return evType // 无点,原样返回
	}
	// 仅当第一段形如 v<数字>(如 v1/v2)时视为版本前缀并剥掉;
	// 否则(如第一段就是 checkout)原样返回。
	if len(seg) > 1 && seg[0] == 'v' && seg[1] >= '0' && seg[1] <= '9' {
		return evType[len(seg)+1:]
	}
	return evType
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
