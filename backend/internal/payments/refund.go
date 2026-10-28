package payments

import (
	"context"
	"fmt"

	"github.com/stripe/stripe-go/v82"
)

// RefundPaymentIntent 通过 Stripe 全额退款(给 PaymentIntent)。
// 成功与否以 Stripe 返回为准;本地状态推进由调用方执行条件更新。
func (c *Client) RefundPaymentIntent(ctx context.Context, paymentIntentID string) error {
	if c.sc == nil {
		return ErrNotConfigured
	}
	_, err := c.sc.V1Refunds.Create(ctx, &stripe.RefundCreateParams{
		PaymentIntent: stripe.String(paymentIntentID),
	})
	if err != nil {
		return fmt.Errorf("stripe refund: %w", err)
	}
	return nil
}
