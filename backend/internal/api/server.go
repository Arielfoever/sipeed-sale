package api

import (
	"log/slog"
	"net/http"

	"sale-sipeed/backend/internal/payments"

	gen "sale-sipeed/backend/internal/api/gen"
	"sale-sipeed/backend/internal/store"
)

// NewServer 组装出完整的根 http.Handler:
//   - /api/*    由 OpenAPI(swagger)生成的路由(oapi-codegen std-http)
//   - /healthz  探活
//
// 前端 dev(vite proxy)与生产(nginx)都把 /api 前缀原样转发到这里,
// 因此路由统一挂在 /api 下,不做路径改写。
func NewServer(st *store.Store, pays *payments.Client, base string, log *slog.Logger) http.Handler {
	mux := http.NewServeMux()

	// 探活(不在 OpenAPI 规范内,运维用途)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	// swagger 生成的 API 路由,挂载在 /api base 下
	return gen.HandlerFromMuxWithBaseURL(NewHandler(st, pays, base, log), mux, "/api")
}
