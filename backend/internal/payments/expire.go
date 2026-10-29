package payments

import (
	"context"
	"strings"

	"github.com/stripe/stripe-go/v82"
)

// ExpireCheckoutSession 让 Checkout Session 过期,防止用户在已取消订单的
// 旧付款链接上继续支付。
// 幂等语义:session 已过期时 Stripe 返回错误,这里视为成功(交易终态一致)。
func (c *Client) ExpireCheckoutSession(ctx context.Context, sessionID string) error {
	if c.sc == nil {
		return ErrNotConfigured
	}
	if sessionID == "" {
		// 无会话(理论不发生:PENDING 必有 session)→ 视为无需处理
		return nil
	}
	_, err := c.sc.V1CheckoutSessions.Expire(ctx, sessionID, &stripe.CheckoutSessionExpireParams{})
	if err != nil {
		// 已过期/已支付等情况按幂等成功处理(订单本地状态由条件更新裁决)
		if isExpireAlreadyDone(err) {
			return nil
		}
		return err
	}
	return nil
}

func isExpireAlreadyDone(err error) bool {
	// 原样匹配官方报错文案:session 已过期、已 completed(无 pending 状态可退)
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "already expired") ||
		strings.Contains(msg, "already completed")
}
