/**
 * API 客户端 —— 由 OpenAPI 规范(openapi.yaml)生成,不再手写请求层。
 *
 * 生成流程:npm run gen:api
 *   ../openapi.yaml --openapi-typescript--> src/api/schema.d.ts
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
import type { CreateOrderResponse, Product } from "./types";

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
        ? "后端尚未提供 /api/orders(404):此为第 1 步的预期 placeholder。"
        : describe(response.status, detail);
    throw new ApiError(
      `${message} 请求 ID:${requestId}`,
      response.status,
    );
  }
  // 类型层面收窄:2xx 时 data 必然存在(openapi-fetch 保证)
  return data;
}
