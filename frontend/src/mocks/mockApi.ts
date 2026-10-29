import type { Plugin } from "vite";
import type { IncomingMessage, ServerResponse } from "node:http";
import type { Product } from "../types";
import { v7 as uuidV7 } from "uuid";

/**
 * 仅用于本地开发的 /api mock 中间件(apply: "serve",不影响生产构建)。
 *
 * 设计目标:
 * 1. 与真实后端保持同一套契约(端点、请求/响应体、整数金额),
 *    mock 返回体直接镜像 Go 后端的响应结构;
 * 2. 下单价格同样由"服务端"按商品单价 × 数量计算——从前端拿到的
 *    mock 响应里 sum 一分不多一分不少,用于自证"前端不传金额"的约定;
 * 3. 回显 X-Request-Id,方便联调幂等头。
 *
 * 真实后端就绪后,不再需要本插件:直接 npm run dev 即可。
 */

const MOCK_PRODUCTS: Product[] = [
  {
    id: "01a181d1-d53a-7428-811c-e53c0548c643",
    name: "LicheeRV Nano",
    description:
      "SG2002 RISC-V SoC 开发板,板载 0.5TOPS NPU 与 1GB 内存,适合边缘 AI 视觉应用。",
    price: 21900,
    currency: "CNY",
    stock: 100,
  },
  {
    id: "01a181d1-d541-7878-b7c5-9e91dd9a6c50",
    name: "Tang Nano 9K",
    description:
      "GW1NR-9 FPGA 板卡,9k LUT,板载 HDMI 与 BL616 无线芯片,FPGA 入门首选。",
    price: 10900,
    currency: "CNY",
    stock: 50,
  },
  {
    id: "01a181d1-d541-76f3-939f-5b3eac92c10f",
    name: "MaixCube",
    description:
      "K210 AI 开发套件,集成摄像头、屏幕与电池,开箱即跑人脸识别 Demo。",
    price: 19800,
    currency: "CNY",
    stock: 0,
  },
];

const UUID_V7_RE =
  /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i;

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => {
    setTimeout(resolve, ms);
  });
}

/** 读取并解析 JSON 请求体 */
function readJsonBody(req: IncomingMessage): Promise<unknown> {
  return new Promise((resolve, reject) => {
    const chunks: Buffer[] = [];
    req.on("data", (chunk: Buffer) => {
      chunks.push(chunk);
    });
    req.on("end", () => {
      try {
        resolve(JSON.parse(Buffer.concat(chunks).toString("utf8")));
      } catch {
        reject(new Error("invalid JSON body"));
      }
    });
    req.on("error", reject);
  });
}

function sendJson(
  res: ServerResponse,
  status: number,
  payload: unknown,
  requestId?: string | null,
): void {
  res.statusCode = status;
  res.setHeader("Content-Type", "application/json; charset=utf-8");
  if (requestId) {
    res.setHeader("X-Request-Id", requestId);
  }
  res.end(JSON.stringify(payload));
}

export function mockApiPlugin(): Plugin {
  return {
    name: "sale-sipeed-mock-api",
    // 只挂在 dev server 上,build/preview 不受影响
    apply: "serve",
    configureServer(server) {
      server.middlewares.use("/api", (req, res, next) => {
        void (async () => {
          // 一点延迟,让 loading 态可观
          await sleep(300);

          const method = req.method ?? "GET";
          const url = (req.url ?? "/").split("?")[0] as string;

          // GET /api/products —— 商品列表
          if (method === "GET" && (url === "/products" || url === "/")) {
            sendJson(res, 200, MOCK_PRODUCTS);
            return;
          }

          // POST /api/orders —— 下单(镜像 Go 后端的请求/响应契约)
          if (method === "POST" && url === "/orders") {
            const rawRequestId = req.headers["x-request-id"];
            const requestId = Array.isArray(rawRequestId)
              ? rawRequestId[0]
              : rawRequestId;

            let body: unknown;
            try {
              body = await readJsonBody(req);
            } catch {
              sendJson(res, 400, { error: "invalid JSON body" }, requestId);
              return;
            }

            const { product_id, quantity } = body as {
              product_id?: number;
              quantity?: number;
            };

            if (
              typeof product_id !== "string" ||
              !UUID_V7_RE.test(product_id) ||
              typeof quantity !== "number" ||
              !Number.isInteger(quantity) ||
              quantity < 1
            ) {
              sendJson(
                res,
                400,
                { error: "product_id 必须为 UUIDv7,quantity 必须为正整数" },
                requestId,
              );
              return;
            }

            const product = MOCK_PRODUCTS.find(
              (p) => p.id === product_id,
            );
            if (!product) {
              sendJson(res, 404, { error: "商品不存在" }, requestId);
              return;
            }
            if (quantity > product.stock) {
              sendJson(res, 409, { error: "库存不足" }, requestId);
              return;
            }

            sendJson(
              res,
              201,
              {
                // 订单号 = UUIDv7(时间戳有序),与真实后端一致
                id: uuidV7(),
                // 与真实后端一致的初始状态;webhook 推进为 PAID
                status: "PENDING",
                // 金额由"服务端"(此处为 mock)计算,前端无从插手
                total_price: product.price * quantity,
                currency: product.currency,
              },
              requestId,
            );
            return;
          }

          // 未匹配到的路径交还给 Vite(会走 proxy 或返回 404)
          next();
        })().catch((err: unknown) => {
          // 兜底:避免未处理的异常挂掉 dev server
          sendJson(res, 500, {
            error: `mock middleware error: ${(err as Error).message}`,
          });
        });
      });
    },
  };
}
