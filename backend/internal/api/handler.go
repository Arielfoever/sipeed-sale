// Package api 承载 HTTP 层:handler 实现由 openapi.yaml 经 oapi-codegen
// 生成(swagger 生成)的 api.ServerInterface,契约变更会在编译期暴露。
package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
	"github.com/oapi-codegen/runtime/types"

	gen "sale-sipeed/backend/internal/api/gen"
	"sale-sipeed/backend/internal/store"
)

// Handler 实现 gen.ServerInterface。
type Handler struct {
	store *store.Store
	log   *slog.Logger
}

// NewHandler 由 swagger 生成的接口 + 数据层组合出 HTTP handler。
func NewHandler(st *store.Store, log *slog.Logger) *Handler {
	return &Handler{store: st, log: log}
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

// CreateOrder POST /orders —— 创建预售订单。
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

	resp := toGenCreateOrderResponse(order)
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
