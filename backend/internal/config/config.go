// Package config 读取环境变量(优先 backend/.env 中的值)。
// 覆盖数据库连接、迁移开关、Stripe 凭据与站点公网地址;
// 真实密钥不入库、不提交 git(.env 已被 .gitignore 排除)。
package config

import (
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

type Config struct {
	// Port HTTP 服务端口
	Port string
	// DatabaseURL PostgreSQL 连接串
	DatabaseURL string
	// AutoMigrate 启动时自动执行 golang-migrate 迁移(dev 默认 true,
	// 生产建议由发布流程执行 migrate 后置 false)
	AutoMigrate bool
	// StripeAPIKey Stripe 沙箱/生产 API Key(sk_test_... / sk_live_...)
	StripeAPIKey string
	// PublicBaseURL 前端站点的公网地址,用于拼接 Stripe 的
	// success_url / cancel_url(浏览器回跳,仅展示,不修改订单状态)
	PublicBaseURL string
	// StripeWebhookSecret Stripe webhook 签名密钥(whsec_...,stripe-cli 或后台获取)
	StripeWebhookSecret string
}

func Load() Config {
	// 缺失 .env 不视为错误(生产环境直接注入环境变量)
	_ = godotenv.Load()

	return Config{
		Port:        getenv("SERVER_PORT", "8080"),
		DatabaseURL: getenv("DATABASE_URL", "postgres://sale_sipeed:sale_sipeed@localhost:5433/sale_sipeed?sslmode=disable"),
		AutoMigrate: getenvBool("MIGRATIONS_AUTO", true),

		StripeAPIKey:        os.Getenv("STRIPE_API_KEY"),
		PublicBaseURL:       getenv("PUBLIC_BASE_URL", "http://localhost:5173"),
		StripeWebhookSecret: os.Getenv("STRIPE_WEBHOOK_SECRET"),
	}
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getenvBool(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return b
}
