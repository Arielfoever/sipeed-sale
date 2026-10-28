// 预售站点后端入口:组装 config / store(pgx + migrate)/ swagger 生成
// 的 API 服务。
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"sale-sipeed/backend/internal/api"
	"sale-sipeed/backend/internal/config"
	"sale-sipeed/backend/internal/payments"
	"sale-sipeed/backend/internal/store"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	cfg := config.Load()

	ctx := context.Background()
	st, err := store.New(ctx, cfg.DatabaseURL, cfg.AutoMigrate)
	if err != nil {
		log.Error("database init", "err", err)
		os.Exit(1)
	}
	defer st.Close()
	log.Info("database ready")

	pays := payments.New(cfg.StripeAPIKey, cfg.StripeWebhookSecret)
	if cfg.StripeAPIKey == "" {
		log.Warn("STRIPE_API_KEY 未配置,下单后无法创建支付链接")
	}
	if cfg.StripeWebhookSecret == "" {
		log.Warn("STRIPE_WEBHOOK_SECRET 未配置,webhook 回调将被拒收")
	}

	server := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           api.NewServer(st, pays, cfg.PublicBaseURL, log),
		ReadHeaderTimeout: 5 * time.Second,
	}

	// 优雅退出
	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-done
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	log.Info("http server listening", "port", cfg.Port)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Error("listenAndServe", "err", err)
		os.Exit(1)
	}
	log.Info("http server stopped")
}
