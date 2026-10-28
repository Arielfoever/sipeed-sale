// Package payments 封装 Stripe 集成(官方 stripe-go SDK,api/v82)。
// 第 3 步:Checkout Session 创建;第 4 步:webhook 签名验证;第 5 步:退款。
package payments

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/stripe/stripe-go/v82"
)

// ErrNotConfigured 未配置 STRIPE_API_KEY(本地起服务时可能没有沙箱密钥)。
var ErrNotConfigured = errors.New("payments: STRIPE_API_KEY not configured")

// Session 是 Checkout Session 的最小结果。
type Session struct {
	ID  string
	URL string
}

// Client 持有 stripe 官方 client;apiKey 为空时处于未配置状态。
type Client struct {
	sc *stripe.Client
}

// New 创建 Stripe 客户端;apiKey 为空表示未配置。
func New(apiKey string) *Client {
	if apiKey == "" {
		return &Client{}
	}
	return &Client{sc: stripe.NewClient(apiKey)}
}

// CheckoutInfo 建立一个 Checkout Session 所需的信息。
// 金额一律为最小货币单位(分),由服务端订单数据授予,绝不来自前端。
type CheckoutInfo struct {
	OrderID     uuid.UUID
	ProductName string
	UnitAmount  int64  // 分
	Currency    string // 如 CNY,内部转小写
	Quantity    int64
	SuccessURL  string // 浏览器回跳(仅展示状态,不修改订单)
	CancelURL   string
}

// CreateCheckoutSession 为一笔 PENDING 订单建立 Stripe Checkout Session。
func (c *Client) CreateCheckoutSession(ctx context.Context, info CheckoutInfo) (*Session, error) {
	if c.sc == nil {
		return nil, ErrNotConfigured
	}

	params := &stripe.CheckoutSessionCreateParams{
		Mode: stripe.String(string(stripe.CheckoutSessionModePayment)),
		LineItems: []*stripe.CheckoutSessionCreateLineItemParams{
			{
				Quantity: stripe.Int64(info.Quantity),
				PriceData: &stripe.CheckoutSessionCreateLineItemPriceDataParams{
					Currency:   stripe.String(strings.ToLower(info.Currency)),
					UnitAmount: stripe.Int64(info.UnitAmount),
					ProductData: &stripe.CheckoutSessionCreateLineItemPriceDataProductDataParams{
						Name: stripe.String(info.ProductName),
					},
				},
			},
		},
		SuccessURL: stripe.String(info.SuccessURL),
		CancelURL:  stripe.String(info.CancelURL),
		// 把订单号写进 Stripe 数据,便于后台核对、退款与入站客服查询
		ClientReferenceID: stripe.String(info.OrderID.String()),
		Metadata:          map[string]string{"order_id": info.OrderID.String()},
	}

	sess, err := c.sc.V1CheckoutSessions.Create(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("create checkout session: %w", err)
	}
	return &Session{ID: sess.ID, URL: sess.URL}, nil
}
