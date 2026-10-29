/**
 * 领域类型 —— 全部从 OpenAPI 规范生成的 schema 派生(./schema.d.ts
 * 由 `npm run gen:api` 基于 ../openapi.yaml 生成)。
 *
 * 这里只保留展示层的格式化工具,不再手写业务类型,避免与 API 契约产生
 * 两个事实源。
 */

import type { components } from "./schema";

/** 商品(GET /api/products 元素) */
export type Product = components["schemas"]["Product"];

/** 下单请求体(POST /api/orders) */
export type CreateOrderPayload = components["schemas"]["CreateOrderPayload"];

/** 下单响应体 */
export type CreateOrderResponse =
  components["schemas"]["CreateOrderResponse"];

/** 订单视图(按订单号查询 / 退款端点返回) */
export type OrderView = components["schemas"]["OrderView"];

/** 把最小货币单位整数格式化为展示文本,例如 219900 -> "¥2,199.00"(CNY) */
export function formatPrice(price: number, currency: string): string {
  try {
    return new Intl.NumberFormat("zh-CN", {
      style: "currency",
      currency,
      minimumFractionDigits: 2,
    }).format(price / 100);
  } catch {
    // 非法货币码时退化为简单格式
    const symbol = currency === "CNY" ? "¥" : `${currency} `;
    return `${symbol}${(price / 100).toFixed(2)}`;
  }
}
