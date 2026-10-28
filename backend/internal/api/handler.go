// Package api 承载 HTTP 层:handler 实现由 openapi.yaml 经 oapi-codegen
// 生成(swagger 生成)的 api.ServerInterface,契约变更会在编译期暴露。
package api

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/oapi-codegen/runtime/types"
	"github.com/stripe/stripe-go/v82"

	gen "sale-sipeed/backend/internal/api/gen"
	"sale-sipeed/backend/internal/payments"
	"sale-sipeed/backend/internal/store"
)

// Handler 实现 gen.ServerInterface。
type Handler struct {
	store *store.Store
	pays  *payments.Client
	// base 前端站点公网地址(PUBLIC_BASE_URL),用于拼 success/cancel URL
	base string
	log  *slog.Logger
}

// NewHandler 由 swagger 生成的接口 + 数据层组合出 HTTP handler。
func NewHandler(st *store.Store, pays *payments.Client, base string, log *slog.Logger) *Handler {
	return &Handler{store: st, pays: pays, base: base, log: log}
}

// ListProducts GET /products —— 预售商品列表。
func (h *Handler) ListProducts(w http.ResponseWriter, r *http.Request) {
	products, err := h.store.ListProducts(r.Context())
	if err != nil {
		h.log.ErrorContext(r.Context(), "list products", "err", err)
		writeError(w, http.StatusInternalServerError, "获取商品失败")
		return
	}
	list := make([]gen.Product, 0, len(products))
	for _, p := range products {
		list = append(list, toGenProduct(p))
	}
	writeJSON(w, http.StatusOK, &list)
}

// CreateOrder POST /orders —— 创建预售订单并返回 Stripe Checkout 地址。
func (h *Handler) CreateOrder(w http.ResponseWriter, r *http.Request, params gen.CreateOrderParams) {
	// 回显请求 ID,便于联调排查(浏览器 devtools / 网关日志)
	if params.XRequestId != nil {
		w.Header().Set("X-Request-Id", params.XRequestId.String())
	}

	var payload gen.CreateOrderJSONRequestBody
	// body 解析失败(非 JSON / 类型不符)→ 400
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "请求体必须是 product_id(quantity 必填)的 JSON")
		return
	}
	if params.XRequestId != nil {
		h.log.DebugContext(r.Context(), "create order",
			"product_id", payload.ProductId, "quantity", payload.Quantity,
			"request_id", params.XRequestId.String())
	}

	in := store.CreateOrderInput{
		ProductID: uuid.UUID(payload.ProductId),
		Quantity:  payload.Quantity,
	}
	if params.XRequestId != nil {
		rid := uuid.UUID(*params.XRequestId)
		in.RequestID = &rid
	}

	order, err := h.store.CreateOrder(r.Context(), in)
	switch {
	case errors.Is(err, store.ErrProductNotFound):
		writeError(w, http.StatusNotFound, "商品不存在")
		return
	case errors.Is(err, store.ErrInsufficientStock):
		writeError(w, http.StatusConflict, "库存不足")
		return
	case err != nil:
		h.log.ErrorContext(r.Context(), "create order", "err", err)
		writeError(w, http.StatusInternalServerError, "下单失败")
		return
	}

	// 第 3 步:为 PENDING 订单准备 Stripe Checkout 链接。
	// - 订单已存在 session(幂等路径)→ 直接复用已存 URL;
	// - 尚无 session → 创建新 session 并落库;
	// - 复用/创建失败均不影响订单本身(仍 PENDING,可重试),返回 502。
	checkoutURL := ""
	if order.Status == "PENDING" {
		if order.StripeSessionURL != nil {
			checkoutURL = *order.StripeSessionURL
		} else {
			var product store.Product
			product, err = h.store.GetProduct(r.Context(), order.ProductID)
			if err != nil {
				h.log.ErrorContext(r.Context(), "load product for checkout", "err", err)
				writeError(w, http.StatusInternalServerError, "下单失败")
				return
			}

			sess, payErr := h.pays.CreateCheckoutSession(r.Context(), payments.CheckoutInfo{
				OrderID:     order.ID,
				ProductName: product.Name,
				UnitAmount:  order.UnitPrice,
				Currency:    order.Currency,
				Quantity:    order.Quantity,
				SuccessURL:  h.base + "/?status=success&order_id=" + order.ID.String(),
				CancelURL:   h.base + "/?status=cancel&order_id=" + order.ID.String(),
			})
			switch {
			case errors.Is(payErr, payments.ErrNotConfigured):
				h.log.ErrorContext(r.Context(), "stripe not configured; cannot checkout")
				writeError(w, http.StatusServiceUnavailable, "支付通道未配置(STRIPE_API_KEY)")
				return
			case payErr != nil:
				h.log.ErrorContext(r.Context(), "create checkout session", "err", payErr)
				writeError(w, http.StatusBadGateway, "创建支付链接失败,请稍后重试")
				return
			}

			if err = h.store.AttachStripeSession(r.Context(), order.ID, sess.ID, sess.URL); err != nil {
				h.log.ErrorContext(r.Context(), "attach stripe session", "err", err)
				writeError(w, http.StatusInternalServerError, "下单失败")
				return
			}
			checkoutURL = sess.URL
		}
	}

	resp := toGenCreateOrderResponse(order)
	resp.CheckoutUrl = checkoutURL
	writeJSON(w, http.StatusCreated, &resp)
}

func toGenProduct(p store.Product) gen.Product {
	return gen.Product{
		Id:          types.UUID(p.ID),
		Name:        p.Name,
		Description: p.Description,
		Price:       p.Price,
		Currency:    p.Currency,
		Stock:       p.Stock,
		ImageUrl:    p.ImageURL,
	}
}

func toGenCreateOrderResponse(o store.Order) gen.CreateOrderResponse {
	return gen.CreateOrderResponse{
		Id:         types.UUID(o.ID),
		Status:     o.Status,
		TotalPrice: o.TotalPrice,
		Currency:   o.Currency,
	}
}

// writeJSON 通用 JSON 应答。
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, &gen.Error{Error: msg})
}

// maxWebhookBody Stripe 事件体上限(保护性限制:目前事件体 <1MB)。
const maxWebhookBody = 1 << 20

// StripeWebhook POST /webhooks/stripe —— 订单状态唯一的进入通道。
// 签名校验使用官方 SDK(webhook.ConstructEventWithOptions),
// 事件映射见 payments.DecideMapping;重复投递由条件更新天然幂等。
func (h *Handler) StripeWebhook(w http.ResponseWriter, r *http.Request, params gen.StripeWebhookParams) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxWebhookBody+1))
	if err != nil || len(body) == 0 {
		writeError(w, http.StatusBadRequest, "请求体为空或不可读")
		return
	}
	if len(body) > maxWebhookBody {
		writeError(w, http.StatusBadRequest, "请求体过大")
		return
	}

	ev, err := h.pays.VerifyWebhook(body, params.StripeSignature)
	switch {
	case errors.Is(err, payments.ErrWebhookNotConfigured):
		h.log.ErrorContext(r.Context(), "stripe webhook not configured (STRIPE_WEBHOOK_SECRET)")
		writeError(w, http.StatusServiceUnavailable, "webhook 未配置签名密钥")
		return
	case err != nil:
		// 签名不匹配/格式错误:不暴露细节,返回 400 让 Stripe 重试策略处理
		h.log.WarnContext(r.Context(), "stripe webhook signature invalid", "err", err)
		writeError(w, http.StatusBadRequest, "webhook 签名校验失败")
		return
	}
	h.log.InfoContext(r.Context(), "stripe webhook received", "type", string(ev.Type), "id", ev.ID)

	// 只解码 checkout.session 事件;其他类型一律忽略(200)
	switch ev.Type {
	case stripe.EventTypeCheckoutSessionCompleted,
		stripe.EventTypeCheckoutSessionAsyncPaymentSucceeded,
		stripe.EventTypeCheckoutSessionAsyncPaymentFailed,
		stripe.EventTypeCheckoutSessionExpired:
	default:
		writeJSON(w, http.StatusOK, map[string]bool{"received": true})
		return
	}

	var cs stripe.CheckoutSession
	if err := json.Unmarshal(ev.Data.Raw, &cs); err != nil || cs.ID == "" {
		h.log.ErrorContext(r.Context(), "unmarshal checkout session", "err", err)
		// Stripe 会重试;这里按 400 明确回执异常结构
		writeError(w, http.StatusBadRequest, "webhook 数据结构异常")
		return
	}

	newStatus, paymentIntent, apply := payments.DecideMapping(ev.Type, &cs)
	if !apply {
		// completed 但异步支付未完成:维持 PENDING,等待 async 事件
		writeJSON(w, http.StatusOK, map[string]bool{"received": true})
		return
	}

	affected, err := h.store.MarkOrderStatusBySession(r.Context(), cs.ID, newStatus, paymentIntent)
	switch {
	case err != nil:
		h.log.ErrorContext(r.Context(), "mark order status", "err", err, "session", strings.TrimSpace(cs.ID), "status", newStatus)
		// 500 → Stripe 重试投递(幂等条件更新保证安全)
		writeError(w, http.StatusInternalServerError, "处理回调失败")
		return
	case affected == 1:
		h.log.InfoContext(r.Context(), "order updated via webhook", "status", newStatus, "session", cs.ID)
	}

	// affected==0:重复投递/已处理 → 幂等成功应答
	writeJSON(w, http.StatusOK, map[string]bool{"received": true})
}
