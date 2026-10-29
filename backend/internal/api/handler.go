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

// GetOrder GET /orders/{order_id} —— 订单号查询(支付核对 / 退款前确认)。
func (h *Handler) GetOrder(w http.ResponseWriter, r *http.Request, orderId types.UUID, params gen.GetOrderParams) {
	// 回显请求 ID,便于联调排查
	if params.XRequestId != nil {
		w.Header().Set("X-Request-Id", params.XRequestId.String())
	}
	id := uuid.UUID(orderId)
	// replyOrderView 已处理 200/404/500
	if _, ok := h.replyOrderView(w, r, id); !ok {
		return
	}
}

// replyOrderView 加载订单并写回 OrderView(JSON);内部已处理 404/500。
// 供 GET /orders/{id} 与退款成功响应共用。
func (h *Handler) replyOrderView(w http.ResponseWriter, r *http.Request, id uuid.UUID) (gen.OrderView, bool) {
	order, productName, err := h.store.GetOrder(r.Context(), id)
	if isNotFoundStoreErr(err) {
		writeError(w, http.StatusNotFound, "订单不存在")
		return gen.OrderView{}, false
	}
	if err != nil {
		h.log.ErrorContext(r.Context(), "get order", "err", err)
		writeError(w, http.StatusInternalServerError, "查询订单失败")
		return gen.OrderView{}, false
	}
	view := toOrderView(order, productName)
	// 统一在此写出 200 响应体(调用方不再各自写,避免遗漏)
	writeJSON(w, http.StatusOK, &view)
	return view, true
}

func isNotFoundStoreErr(err error) bool {
	return errors.Is(err, store.ErrOrderNotFound)
}

func toOrderView(o store.Order, productName string) gen.OrderView {
	return gen.OrderView{
		Id:          types.UUID(o.ID),
		Status:      o.Status,
		TotalPrice:  o.TotalPrice,
		Currency:    o.Currency,
		ProductName: productName,
		Quantity:    o.Quantity,
		CreatedAt:   o.CreatedAt,
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
// 签名校验使用官方 SDK(stripe.ValidatePayload,只验签不解析,避开 CLI
// 测试事件 created 为字符串与 SDK int64 字段不兼容的坑),事件映射见
// payments.DecideMapping;重复投递由条件更新天然幂等。
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
		// 签名不匹配/格式错误:返回 400 让 Stripe 重试策略处理。
		// 绝大多数情况是后端 .env 的 STRIPE_WEBHOOK_SECRET 与 `stripe listen`
		// 打印的 whsec_ 不一致,或改完 .env 没重启 backend 容器。
		h.log.ErrorContext(r.Context(), "stripe webhook signature invalid",
			"err", err.Error(),
			"hint", "对比 backend/.env 的 STRIPE_WEBHOOK_SECRET 与 `stripe listen` 打印的 whsec_;改后需 docker compose up -d backend 重启")
		writeError(w, http.StatusBadRequest, "webhook 签名校验失败")
		return
	}
	h.log.InfoContext(r.Context(), "stripe webhook received", "type", ev.Type, "id", ev.ID)

	// 只处理 checkout.session.* 事件;其他类型一律忽略(200)
	switch ev.Type {
	case string(stripe.EventTypeCheckoutSessionCompleted),
		string(stripe.EventTypeCheckoutSessionAsyncPaymentSucceeded),
		string(stripe.EventTypeCheckoutSessionAsyncPaymentFailed),
		string(stripe.EventTypeCheckoutSessionExpired):
	default:
		writeJSON(w, http.StatusOK, map[string]bool{"received": true})
		return
	}

	// 定位关联的 CheckoutSession ID:thin event 用 related_object.id
	// (官方 thin event 格式,无 data.object);snapshot event 用 data.object.id。
	sessionID := ""
	if ev.RelatedObject != nil {
		sessionID = ev.RelatedObject.ID
	}
	if sessionID == "" && len(ev.Data) > 0 {
		var obj struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(ev.Data, &obj); err == nil {
			sessionID = obj.ID
		}
	}
	if sessionID == "" {
		h.log.ErrorContext(r.Context(), "webhook event missing checkout session id",
			"type", ev.Type, "event", ev.ID)
		writeError(w, http.StatusBadRequest, "webhook 数据结构异常")
		return
	}

	// 按 Stripe 官方 thin event 要求:用 API 拉取完整 CheckoutSession,
	// 取权威的 payment_status / payment_intent(不依赖 body 内嵌对象)。
	cs, err := h.pays.FetchCheckoutSession(r.Context(), sessionID)
	switch {
	case errors.Is(err, payments.ErrNotConfigured):
		h.log.ErrorContext(r.Context(), "stripe not configured; cannot fetch session for webhook")
		writeError(w, http.StatusServiceUnavailable, "支付通道未配置(STRIPE_API_KEY)")
		return
	case err != nil:
		if payments.IsNotFound(err) {
			// 对象已不存在(如 session 被 Stripe 侧删除):按已处理幂等返回
			h.log.WarnContext(r.Context(), "checkout session not found for webhook", "session", sessionID, "err", err)
			writeJSON(w, http.StatusOK, map[string]bool{"received": true})
			return
		}
		h.log.ErrorContext(r.Context(), "fetch checkout session for webhook", "err", err, "session", sessionID)
		// 500 → Stripe 重试投递(幂等条件更新保证安全)
		writeError(w, http.StatusInternalServerError, "处理回调失败")
		return
	}

	newStatus, paymentIntent, apply := payments.DecideMapping(ev.Type, cs)
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

// CancelOrder POST /orders/{order_id}/cancel —— 取消未付款订单(仅 PENDING)。
// 顺序:先让 Stripe Checkout Session 过期(防止用户用旧链接继续付款,失败
// 视为网关错误 502),再本地事务条件推进 PENDING → CANCELED 并归还库存。
// 幂等:非 PENDING 返回 409;订单不存在返回 404。
func (h *Handler) CancelOrder(w http.ResponseWriter, r *http.Request, orderId types.UUID, params gen.CancelOrderParams) {
	if params.XRequestId != nil {
		w.Header().Set("X-Request-Id", params.XRequestId.String())
	}

	id := uuid.UUID(orderId)

	// 先读取订单:仅有 session 的订单需要先在 Stripe 侧作废链接。
	order, _, err := h.store.GetOrder(r.Context(), id)
	if errors.Is(err, store.ErrOrderNotFound) {
		writeError(w, http.StatusNotFound, "订单不存在")
		return
	}
	if err != nil {
		h.log.ErrorContext(r.Context(), "load order for cancel", "err", err)
		writeError(w, http.StatusInternalServerError, "取消订单失败")
		return
	}
	if order.Status != "PENDING" {
		writeError(w, http.StatusConflict, "当前状态不可取消(仅待付款订单可取消)")
		return
	}

	if order.StripeSessionID != nil && *order.StripeSessionID != "" {
		if err := h.pays.ExpireCheckoutSession(r.Context(), *order.StripeSessionID); err != nil {
			if errors.Is(err, payments.ErrNotConfigured) {
				h.log.ErrorContext(r.Context(), "stripe not configured; cannot expire session")
				writeError(w, http.StatusServiceUnavailable, "支付通道未配置(STRIPE_API_KEY)")
				return
			}
			h.log.ErrorContext(r.Context(), "expire checkout session failed", "err", err, "session", *order.StripeSessionID)
			writeError(w, http.StatusBadGateway, "Stripe 会话作废失败,请稍后重试")
			return
		}
	}

	affected, err := h.store.CancelOrder(r.Context(), id)
	switch {
	case errors.Is(err, store.ErrOrderNotFound):
		writeError(w, http.StatusNotFound, "订单不存在")
		return
	case errors.Is(err, store.ErrNotCancelable):
		writeError(w, http.StatusConflict, "当前状态不可取消(仅待付款订单可取消)")
		return
	case err != nil:
		h.log.ErrorContext(r.Context(), "cancel order", "err", err)
		writeError(w, http.StatusInternalServerError, "取消订单失败")
		return
	case affected == 1:
		h.log.InfoContext(r.Context(), "order canceled", "order_id", id.String())
	}

	// 统一返回订单完整视图(含商品名)
	if _, ok := h.replyOrderView(w, r, id); !ok {
		return
	}
}

// RefundOrder POST /orders/{order_id}/refund —— 全额退款(仅 PAID)。
// 条件更新幂等:顺序为 锁单校验 → Stripe Refund → 本地 REFUNDED;
// Stripe 失败返回 502,本地保持 PAID 可重试;Stripe 已成功但本地写库失败时,
// 重试会得到 Stripe already refunded,由运维排查(README 已注明)。
func (h *Handler) RefundOrder(w http.ResponseWriter, r *http.Request, orderId types.UUID, params gen.RefundOrderParams) {
	if params.XRequestId != nil {
		w.Header().Set("X-Request-Id", params.XRequestId.String())
	}

	id := uuid.UUID(orderId)
	pi, err := h.store.LockRefundable(r.Context(), id)
	switch {
	case errors.Is(err, store.ErrOrderNotFound):
		writeError(w, http.StatusNotFound, "订单不存在")
		return
	case errors.Is(err, store.ErrNotRefundable):
		writeError(w, http.StatusConflict, "当前状态不可退款(仅 PAID 可退)")
		return
	case errors.Is(err, store.ErrNoPaymentIntent):
		writeError(w, http.StatusConflict, "订单缺少支付凭据,无法退款")
		return
	case err != nil:
		h.log.ErrorContext(r.Context(), "lock refundable order", "err", err)
		writeError(w, http.StatusInternalServerError, "退款失败")
		return
	}

	if err := h.pays.RefundPaymentIntent(r.Context(), pi); err != nil {
		h.log.ErrorContext(r.Context(), "stripe refund failed", "err", err, "pi", pi)
		writeError(w, http.StatusBadGateway, "Stripe 退款失败,请稍后重试")
		return
	}

	if _, err := h.store.MarkOrderRefunded(r.Context(), id); err != nil {
		// Stripe 已退;本地状态需要运维核对(Stripe 侧退款由其幂等保证)
		h.log.ErrorContext(r.Context(), "mark refunded failed", "err", err)
		writeError(w, http.StatusInternalServerError, "退款成功但本地状态更新失败")
		return
	}

	// 统一返回订单完整视图(含商品名)
	if _, ok := h.replyOrderView(w, r, id); !ok {
		return
	}
}
