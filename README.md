# SaleSipeed — 硬件预售站点

一个可跑通「商品展示 → 下单 → 付款 → 回调确认」全流程的预售站点示例:

- **前端**:React + TypeScript + Vite + Ant Design(`frontend/`)
- **后端**:Go + PostgreSQL(后续步骤,`backend/`)
- **支付**:Stripe Checkout + Webhook(第 3、4 步)

## 当前进度:第 1 步 —— 前端商品页(已实现)

商品数据通过 `GET /api/products` 从后端获取并渲染;点击「下单」会携带随机
请求 ID(`X-Request-Id`,用于防重复点击/幂等排查)向后端发起
`POST /api/orders`(body 仅含 `product_id + quantity`,金额由后端计算,
前端不提交任何金额字段)。

**此阶段后端尚未实现**,使用 `npm run dev:mock` 可在 dev server 内模拟
全部接口;也可以不开启 mock,请求将得到 404 / 连接拒绝,页面会以提示形式
呈现(均为预期行为)。

### API 均由 Swagger/OpenAPI 生成

`openapi.yaml`(仓库根)是前后端共享的 API **单一事实源**,任何接口不是
手写的,而是由它生成:

```
openapi.yaml
   ├── frontend:`npm run gen:api` → src/schema.d.ts → openapi-fetch 类型化客户端
   └── backend(WIP,第 2 步):由同一规范生成 Go 类型与路由 stub
```

- 前端的类型(`schema.d.ts`)+ 请求执行(`openapi-fetch`)均由 spec 生成,
  请求路径/方法/请求体/响应体在编译期即受约束,不会与后端契约漂移;
- spec 变更后运行 `npm run gen:api` 重新生成;生成文件与源码一同提交,
  CI 中会校验生成物是否与 spec 同步(附加项实现);
- 金额为分、只传 `product_id + quantity`、`X-Request-Id` 幂等头等约定
  都已写入 spec 的字段描述与说明中。

### 运行

```bash
cd frontend
npm install
npm run dev        # 真实模式:/api 代理到 http://localhost:8080(Go 后端)
npm run dev:mock   # Mock 模式:dev server 内直接模拟 /api,无需后端
```

打开 http://localhost:5173 即可看到商品页。

### 如何使用Mock

后端尚未实现或想独立联调前端时,使用 **Mock 模式**:

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

## 路线图

| 步骤 | 内容 | tag |
| --- | --- | --- |
| 1 | 前端商品页 + 下单按钮(mock/404) | `step1-frontend` |
| 2 | Go 后端:下单接口、订单存储、价格计算、pgx + golang-migrate | `step2-orders` |
| 3 | Stripe Checkout 跳转付款(沙箱) | `step3-stripe` |
| 4 | Webhook 回调确认订单 | `step4-webhook` |
| 附加 | 退款、docker compose、nginx 部署、CI | — |
