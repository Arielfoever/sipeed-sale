/**
 * API 客户端 —— 由 OpenAPI 规范(openapi.yaml)生成,不再手写请求层。
 *
 * 生成流程:npm run gen:api
 *   ../openapi.yaml --openapi-typescript--> src/schema.d.ts
 *
 * 运行时使用 openapi-fetch(约 1KB,零依赖的多 polyfill 轻客户端),
 * 请求路径/方法/请求体/响应体均由 OpenAPI 类型约束。
 *
 * baseUrl 用相对路径 /api:dev 由 vite proxy 转发,prod 由 nginx 反代到
 * 后端容器(同域),前端不关心 API origin 与部署方式。
 */

import createClient from "openapi-fetch";
import { v7 as uuidV7 } from "uuid";
import type { paths } from "./schema";
import type { CreateOrderResponse, OrderView, Product } from "./types";

type ApiClient = ReturnType<typeof createClient<paths>>;

export class ApiError extends Error {
  readonly status: number;

  constructor(message: string, status: number) {
    super(message);
    this.name = "ApiError";
    this.status = status;
  }
}

const client: ApiClient = createClient<paths>({ baseUrl: "/api" });

/** 生成一次性的随机请求 ID(UUIDv7):防重复下单、便于幂等排查 */
export function newRequestId(): string {
  return uuidV7();
}

/** 错误响应体在 spec 中统一为 { error: string } */
type ErrorBody = { error: string };

function describe(status: number, detail?: ErrorBody): string {
  const base =
    detail?.error ?? (status === 0 ? "网络错误" : `请求失败(HTTP ${status})`);
  return base;
}

/** 拉取商品列表(GET /products) */
export async function fetchProducts(): Promise<Product[]> {
  const { data, error, response } = await client.GET("/products");
  if (error) {
    throw new ApiError(
      describe(response.status, error as ErrorBody),
      response.status,
    );
  }
  return (data ?? []) as Product[];
}

type CreateOrderArgs = {
  product_id: string;
  quantity: number;
};

/** 创建订单(POST /orders);每次调用生成新的随机 X-Request-Id */
export async function createOrder(
  payload: CreateOrderArgs,
): Promise<CreateOrderResponse> {
  const requestId = newRequestId();
  const { data, error, response } = await client.POST("/orders", {
    headers: { "X-Request-Id": requestId },
    body: payload,
  });
  if (error) {
    const detail = error as ErrorBody;
    const message =
      response.status === 404
        ? "商品不存在"
        : describe(response.status, detail);
    throw new ApiError(
      `${message} 请求 ID:${requestId}`,
      response.status,
    );
  }
  if (!data) {
    throw new ApiError(
      `下单失败(后端返回空响应) 请求 ID:${requestId}`,
      (response as Response).status,
    );
  }
  return data;
}

/** 按订单号查询订单(GET /orders/{order_id};webhook 确认后自查入口) */
export async function getOrder(orderId: string): Promise<OrderView> {
  const { data, error, response } = await client.GET("/orders/{order_id}", {
    params: { path: { order_id: orderId } },
  });
  if (error) {
    const detail = error as ErrorBody;
    throw new ApiError(
      detail?.error ?? `查询订单失败(HTTP ${response.status})`,
      response.status,
    );
  }
  // 空 200(后端契约漂移)兜底:明确抛错而不是把 undefined 传出去
  if (!data) {
    throw new ApiError("查询订单失败(后端返回空响应)", (response as Response).status);
  }
  return data;
}

/** 申请全额退款(POST /orders/{order_id}/refund;仅 PAID 可退,后端幂等) */
export async function refundOrder(orderId: string): Promise<OrderView> {
  const requestId = newRequestId();
  const { data, error, response } = await client.POST(
    "/orders/{order_id}/refund",
    {
      params: { path: { order_id: orderId } },
      headers: { "X-Request-Id": requestId },
    },
  );
  if (error) {
    const detail = error as ErrorBody;
    throw new ApiError(
      detail?.error ?? `退款失败(HTTP ${response.status})`,
      response.status,
    );
  }
  if (!data) {
    throw new ApiError("退款失败(后端返回空响应)", (response as Response).status);
  }
  return data;
}

/** 取消未付款订单(POST /orders/{order_id}/cancel;仅 PENDING 可取消,后端幂等) */
export async function cancelOrder(orderId: string): Promise<OrderView> {
  const requestId = newRequestId();
  const { data, error, response } = await client.POST(
    "/orders/{order_id}/cancel",
    {
      params: { path: { order_id: orderId } },
      headers: { "X-Request-Id": requestId },
    },
  );
  if (error) {
    const detail = error as ErrorBody;
    throw new ApiError(
      detail?.error ?? `取消订单失败(HTTP ${response.status})`,
      response.status,
    );
  }
  if (!data) {
    throw new ApiError("取消订单失败(后端返回空响应)", (response as Response).status);
  }
  return data;
}
