# SaleSipeed — 硬件预售站点

一个可跑通「商品展示 → 下单 → 付款 → 回调确认」全流程的预售站点示例:

- **前端**:React + TypeScript + Vite + Ant Design(`frontend/`)
- **后端**:Go + PostgreSQL(后续步骤,`backend/`)
- **支付**:Stripe Checkout + Webhook(第 3、4 步)

## 当前进度:第 2 步 —— Go 后端(已实现)

前端(`frontend/`)负责商品展示与下单;后端(`backend/`,第 2 步)已实现:

- `GET /api/products` 商品列表(pgx + 手写 SQL,SQL 全部集中在
  `backend/internal/store` 单包,便于未来迁移)
- `POST /api/orders` 下单:服务端按商品表价格计算总额(分)、事务 +
  `SELECT FOR UPDATE` 扣库存、订单号 UUIDv7(`google/uuid` v7)、
  `X-Request-Id` 幂等去重(`request_id` 唯一索引,重复提交复用既有订单)
- 数据库迁移:`golang-migrate` + `iofs` 内嵌迁移 SQL(`backend/internal/
  store/migrations/`),启动自动执行(`MIGRATIONS_AUTO=true`,生产可关闭)
- API 服务端代码由 swagger 生成:`openapi.yaml` 经 `oapi-codegen` 生成类型
  与路由,handler 实现该生成接口(契约编译期对齐)

### 后端运行(首次)

```bash
# 快速方式:一键起全栈(PG + 后端 + 前端 nginx,Stripe 凭据可选注入)
docker compose -f docker-compose.dev.yml up -d --build
#   前端 http://localhost:4173 / 后端 http://localhost:8080 / PG localhost:5433

# 或者:仅起 PostgreSQL(仅本机测试;生产用服务器上的 PG,连接串见 .env.example),
# 后端/前端跑在本机:
docker compose -f docker-compose.dev.yml up -d postgres

# 后端配置(按需修改 DATABASE_URL / SERVER_PORT 等)
cd backend && cp .env.example .env

# 生成 swagger 服务端代码并启动
make gen
make run
```

`make run` 自动执行迁移并监听 `:8080`;`curl -s localhost:8080/healthz`
(一键全栈方式下,前端在 `http://localhost:4173`,但 `healthz` 仍需直接访问
后端 `localhost:8080`,`/healthz` 不在 `/api` 前缀下)。

### 验收(第 2 步:curl 落库返回订单号)

```bash
curl -s localhost:8080/api/products | jq

curl -s -X POST localhost:8080/api/orders \
  -H 'Content-Type: application/json' \
  -H "X-Request-Id: $(uuidgen | tr 'A-Z' 'a-z')" \
  -d '{"product_id":"01a181d1-d53a-7428-811c-e53c0548c643","quantity":2}' | jq
# → {"currency":"CNY","id":"<uuidv7>","status":"PENDING","total_price":43800}

# 同一 X-Request-Id 重复提交 → 返回同一订单(id/total 不变,不重复扣库存)
```

订单与库存可在 PG 中核对(`orders` / `products` 表)。

### 前端运行

```bash
cd frontend
npm install
npm run gen:api     # 由根 openapi.yaml 重新生成 TS 客户端类型
npm run dev         # 真实模式:/api 代理到 http://localhost:8080(Go 后端)
npm run dev:mock    # Mock 模式(后端未就绪时联调前端)
```

打开 http://localhost:5173 即可看到商品页;真实模式下下单结果与后端数据库
一致(mock 模式下为模拟数据,响应结构同源)。

### API 均由 Swagger/OpenAPI 生成

`openapi.yaml`(仓库根)是前后端共享的 API **单一事实源**,任何接口不是
手写的,而是由它生成:

```
openapi.yaml
   ├── frontend:`npm run gen:api` → src/schema.d.ts → openapi-fetch 类型化客户端
   └── backend:`backend/Makefile gen`(oapi-codegen)→ 类型 + 路由(挂 /api)
```

- 前端的类型(`schema.d.ts`)+ 请求执行(`openapi-fetch`)均由 spec 生成,
  请求路径/方法/请求体/响应体在编译期即受约束,不会与后端契约漂移;
- 后端 `gen.ServerInterface` 由同一 spec 生成,handler 必须实现它——
  spec 契约变化会在编译期暴露;
- spec 变更后:前端 `npm run gen:api`、后端 `make gen` 重新生成;生成
  文件与源码一同提交,CI 中会校验生成物是否与 spec 同步(附加项实现);
- 金额为分、UUIDv7 ID、`X-Request-Id` 幂等头等约定都已写入 spec 的字段
  描述与说明中。

### 运行

```bash
cd frontend
npm install
npm run dev        # 真实模式:/api 代理到 http://localhost:8080(Go 后端)
npm run dev:mock   # Mock 模式:dev server 内直接模拟 /api,无需后端
```

打开 http://localhost:5173 即可看到商品页。

### 如何使用Mock

后端未启动或想独立联调前端时,使用 **Mock 模式**:

```bash
cd frontend
npm run dev:mock
```

它的实现是一个 Vite dev-server 中间件插件(`src/mocks/mockApi.ts`,
`apply: "serve"`,只作用于 dev,不影响生产构建),支持:

| 端点 | 行为 |
| --- | --- |
| `GET /api/products` | 返回 3 件 Sipeed 风格硬件(含一件缺货),模拟 300ms 延迟演示 loading |
| `POST /api/orders` | 校验 `product_id`/`quantity` 与库存;金额由 mock"服务端"计算返回 `total_price`(分),回显 `X-Request-Id` 头 |

边界情况也会按后端契约返回:`400`(非法参数)、`404`(商品不存在)、
`409`(库存不足)。

要点:

1. **契约同源**:mock 的请求/响应体与第 2 步 Go 后端会保持一致,前端代码
   在两种模式间零改动;
2. **金额自证**:mock 端的下单总额 = 单价 × 数量,由"服务端"算好返回,
   前端从不提交金额——与真实后端同一约定;
3. 也可用环境变量开启:`VITE_API_MOCK=1 npm run dev` 或在
   `frontend/.env.local` 写入 `VITE_API_MOCK=1`;
4. 真实后端就绪后,继续用 `npm run dev`(真实模式),dev server 会把
   `/api` 代理到 `http://localhost:8080`(可用 `VITE_API_PROXY_TARGET`
   覆盖,样例见 `.env.example`)。

### 构建产物

```bash
npm run build   # tsc 类型检查 + vite build,产物在 frontend/dist/
```

## 设计约定

1. **金额只用整数**:一律以最小货币单位(分)存储与传输,前端仅做展示层
   格式化。
2. **不信任前端金额**:下单只提交 `product_id + quantity`,总价由后端按
   商品表价格计算。
3. **ID 一律 UUIDv7**:订单号、`product_id`、`X-Request-Id` 均为 UUIDv7
   (时间戳有序,适合聚簇索引)。前端统一使用官方 `uuid` 包的
   `v7()`(`import { v7 as uuidV7 } from "uuid"`),后端(第 2 步)将使用
   等价实现(如 `google/uuid` 的 v7)保持一致。
4. **幂等请求 ID**:每次点击下单都生成随机 `X-Request-Id`(UUIDv7),防止
   重复点击造成重复下单,便于后端做幂等去重。
5. **订单状态以 webhook 为准**:浏览器跳回 success 页只用于展示,不改
   订单状态(第 4 步实现)。

### 第 3 步验收(Stripe Checkout 跳转)

在 `backend/.env` 填入沙箱密钥后 `make run`:

```bash
curl -s -X POST localhost:8080/api/orders \
  -H 'Content-Type: application/json' \
  -H "X-Request-Id: $(uuidgen | tr 'A-Z' 'a-z')" \
  -d '{"product_id":"01a181d1-d53a-7428-811c-e53c0548c643","quantity":1}' | jq
# → {...,"status":"PENDING","total_price":21900,
#     "checkout_url":"https://checkout.stripe.com/c/pay/..."}
```

前端页面真实模式下点「下单」会自动跳转到 Stripe 沙箱付款页;沙箱用测试卡
`4242 4242 4242 4242`(任意未来日期/CVC)支付后浏览器跳回 `?status=success`
(该回跳**仅展示**,订单状态由第 4 步 webhook 推进)。

### 第 4 步验收(webhook 确认付款)

订单状态**只认 webhook**;浏览器回跳 success 页仅展示。

```bash
# 终端 1:stripe-cli 转发回调(会打印 whsec_...,填进 backend/.env 后重启 make run)
stripe listen \
  --events checkout.session.completed,checkout.session.async_payment_succeeded,checkout.session.async_payment_failed,checkout.session.expired \
  --forward-to localhost:8080/api/webhooks/stripe

# 终端 2:前端(npm run dev)下单 → Stripe 沙箱用 4242 4242 4242 4242 支付
# 终端 1 可见转发,后端日志出现 order updated via webhook status=PAID

# 数据库验证
# psql ... -c "SELECT id, status, stripe_payment_intent_id FROM orders"

# 幂等复验:直接触发一次假事件(无匹配订单 → affected 0 行,仍返回 received:true)
stripe trigger checkout.session.completed
```

事件 → 状态映射(条件更新天然幂等,重复投递命中 0 行):

| 事件 | 状态 |
| --- | --- |
| `checkout.session.completed`(payment_status=paid) | `PAID` |
| `checkout.session.async_payment_succeeded` | `PAID` |
| `checkout.session.async_payment_failed` | `FAILED` |
| `checkout.session.expired` | `CANCELED` |

> **thin / snapshot 事件**:Stripe 默认投递 thin event(`object: v2.core.event`,
> 只有 `related_object` 指针、没有内嵌 `data.object`)。后端按官方要求,在
> 收到 `checkout.session.*` 事件后用 `V1CheckoutSessions.Retrieve` 拉取完整
> CheckoutSession(权威 `payment_status` / `payment_intent`),再推进订单状态;
> snapshot event(内嵌 `data.object`)同样走此路径,两种格式均可处理。

### 第 5 步验收(退款 / 部署 / CI)

**退款**(示例接口未内置管理员鉴权,生产须在网关或加 JWT 后使用):

```bash
# 1) 订单号查询(支付结果核对 / 退款前确认)
curl -s localhost:8080/api/orders/<订单号> -H "X-Request-Id: $(uuidgen | tr 'A-Z' 'a-z')" | jq
# → {"id":"...","status":"PAID","total_price":...,"currency":"CNY",
#    "product_name":"LicheeRV Nano","quantity":1,"created_at":"..."}

# 2) 对 PAID 订单申请全额退款
curl -s -X POST localhost:8080/api/orders/<订单号>/refund \
  -H "X-Request-Id: $(uuidgen | tr 'A-Z' 'a-z')" | jq
# → {"id":"...","status":"REFUNDED",...}
# 终态幂等:重复调用 → 409(仅 PAID 可退);Stripe 失败 → 502,本地仍 PAID
```

前端(商品页上方「订单查询」卡片):粘贴订单号查询,弹窗展示购买内容;
`PAID` 订单可点「申请退款」,退款后弹窗状态变为 `REFUNDED`;`PENDING`
订单可点「取消订单」(二次确认),取消后状态变为 `CANCELED` 并归还库存。

**取消未付款订单**(仅 PENDING 可取消):

```bash
curl -s -X POST localhost:8080/api/orders/<订单号>/cancel \
  -H "X-Request-Id: $(uuidgen | tr 'A-Z' 'a-z')" | jq
# → {"id":"...","status":"CANCELED",...,"product_name":"...","quantity":1}
# 幂等语义:非 PENDING → 409;订单不存在 → 404;
# Cancel 流程:stripe ExpireCheckoutSession(已过期/已完成的按幂等成功处理,
# 防止用户用旧链接继续付款)→ 本地事务条件更新 PENDING→CANCELED 并归还库存;
# Stripe 会话作废失败 → 502,本地保持 PENDING 可重试。
```

**服务器部署**(前端由宿主机 nginx 托管、后端转发到 docker 内):

> 📄 **完整上线步骤见 [`DEPLOY.md`](DEPLOY.md)**(含服务器 PostgreSQL 初始化、
> `.env` 密钥、HTTPS/certbot、Stripe 生产 webhook、备份与升级流程)。

```bash
cd frontend && npm run build          # dist/ → scp 到 /opt/sale-sipeed/dist
cp nginx/sale-sipeed.conf /etc/nginx/conf.d/ && nginx -t && systemctl reload nginx
docker compose up -d --build          # 依赖 .env 提供密钥(见 docker-compose.yml)
```

**CI**:`.github/workflows/ci.yml`,每个 PR/push:
Go(gofmt / vet / build / test / swagger 生成物漂移检查)— 前端(npm ci /
生成物漂移 / tsc+vite build / 上传 dist 产物)— 依赖前两者成功后构建后端
Docker 镜像(不推送)。

## 路线图

| 步骤 | 内容 | tag |
| --- | --- | --- |
| 1 | 前端商品页 + 下单按钮(mock/404) | `step1-frontend` ✅ |
| 2 | Go 后端:下单接口、订单存储、价格计算、pgx 手写 SQL 单包、golang-migrate、oapi-codegen(swagger) | `step2-orders` |
| 3 | Stripe Checkout 跳转付款(沙箱) | `step3-stripe` |
| 4 | Webhook 回调确认订单 | `step4-webhook` |
| 附加 | 退款、docker compose、nginx 部署、CI | — |
