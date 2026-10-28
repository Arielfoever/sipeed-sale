# 后端常用命令(Go + oapi-codegen + golang-migrate)
.PHONY: gen build run test vet tidy

gen: ## 由 openapi.yaml(仓库根)重新生成 swagger 服务端代码
	go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.4.1 \
		-config internal/api/gen.cfg.yaml ../openapi.yaml

build: ## 编译检查
	go build ./...

vet: ## go vet 静态检查
	go vet ./...

test: ## 单元测试(后续步骤补充)
	go test ./...

run: ## 本地启动(需先有 PostgreSQL,可 docker compose -f docker-compose.dev.yml up -d)
	go run ./cmd/server

tidy:
	go mod tidy
