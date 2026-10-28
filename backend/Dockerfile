# syntax=docker/dockerfile:1
# 多阶段构建:静态编译 → 运行镜像(alpine,非 root)
# 迁移 SQL 以 ioFS 内嵌进二进制,运行镜像无需额外文件。
FROM golang:1.27-alpine AS build
WORKDIR /src
# 先拷贝依赖清单,充分利用层缓存
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/sale-sipeed-api ./cmd/server

FROM alpine:3.20 AS runtime
RUN addgroup -S app && adduser -S -G app app \
    && apk add --no-cache ca-certificates
COPY --from=build /out/sale-sipeed-api /usr/local/bin/sale-sipeed-api
USER app
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/sale-sipeed-api"]
