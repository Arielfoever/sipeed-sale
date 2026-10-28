import { useCallback, useEffect, useState } from "react";
import type { ReactElement } from "react";
import {
  Alert,
  Button,
  Card,
  ConfigProvider,
  Empty,
  Flex,
  InputNumber,
  Spin,
  Tag,
  Typography,
  theme,
} from "antd";
import zhCN from "antd/locale/zh_CN";
import { ApiError, createOrder, fetchProducts } from "./api";
import { formatPrice, type CreateOrderResponse, type Product } from "./types";

type LoadState =
  | { phase: "loading" }
  | { phase: "error"; message: string }
  | { phase: "ready"; products: Product[] };

interface QuantityMap {
  readonly [productId: string]: number;
}

interface PendingMap {
  readonly [productId: string]: true;
}

/** productId -> 错误提示文本 */
interface OrderErrorMap {
  readonly [productId: string]: string;
}

interface OrderResultMap {
  readonly [productId: string]: CreateOrderResponse;
}

export function App(): ReactElement {
  const [state, setState] = useState<LoadState>({ phase: "loading" });
  const [quantities, setQuantities] = useState<QuantityMap>({});
  const [pending, setPending] = useState<PendingMap>({});
  const [orderErrors, setOrderErrors] = useState<OrderErrorMap>({});
  const [orderResults, setOrderResults] = useState<OrderResultMap>({});
  // 浏览器从 Stripe 回跳(仅展示,订单状态以 webhook 为准,见第 4 步)
  const [returnBanner, setReturnBanner] = useState<string | null>(() => {
    const params = new URLSearchParams(window.location.search);
    const status = params.get("status");
    const orderId = params.get("order_id");
    if (status === "success" && orderId) {
      return `订单 ${orderId}:Stripe 已接受付款,订单状态以后端 webhook 确认为准。`;
    }
    if (status === "cancel" && orderId) {
      return `订单 ${orderId}:你取消了支付,订单仍是 PENDING,可重新下单。`;
    }
    return null;
  });

  // 清理回跳参数,避免刷新时重复提示
  useEffect(() => {
    if (returnBanner) {
      window.history.replaceState(null, "", window.location.pathname);
    }
  }, [returnBanner]);

  useEffect(() => {
    let cancelled = false;
    fetchProducts()
      .then((products) => {
        if (cancelled) return;
        const initial: Record<string, number> = {};
        for (const p of products) initial[p.id] = 1;
        setQuantities(initial);
        setState({ phase: "ready", products });
      })
      .catch((err: unknown) => {
        if (cancelled) return;
        setState({
          phase: "error",
          message:
            err instanceof Error ? err.message : "获取商品失败(未知错误)",
        });
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const setQuantity = useCallback((productId: string, value: number | null) => {
    const clamped = Math.max(1, value ?? 1);
    setQuantities((prev) => ({ ...prev, [productId]: clamped }));
  }, []);

  const handleOrder = useCallback(
    async (product: Product) => {
      const quantity = quantities[product.id] ?? 1;
      setOrderErrors((prev) => {
        const next = { ...prev };
        delete next[product.id];
        return next;
      });
      setOrderResults((prev) => {
        const next = { ...prev };
        delete next[product.id];
        return next;
      });
      setPending((prev) => ({ ...prev, [product.id]: true }));
      try {
        // 第 1 步契约:前端发起 POST /api/orders。
        // 后端尚未实现时,这里得到 ECONNREFUSED(代理拒绝)或 404,
        // 均视为预期中的 mock 阶段行为并向用户明确提示。
        const order = await createOrder({
          product_id: product.id,
          quantity,
        });
        setOrderResults((prev) => ({ ...prev, [product.id]: order }));
        // 第 3 步约定:拿到后端返回的 Checkout URL 后跳转 Stripe 付款页
        if (order.checkout_url) {
          window.location.assign(order.checkout_url);
        }
      } catch (err) {
        const status = err instanceof ApiError ? err.status : undefined;
        const message =
          err instanceof ApiError
            ? status === 404
              ? "后端尚未提供 /api/orders(404):此为第 1 步的预期 placeholder。"
              : err.message
            : "下单请求发送失败:后端服务不可用(请确认后端已启动)。";
        setOrderErrors((prev) => ({ ...prev, [product.id]: message }));
      } finally {
        setPending((prev) => {
          const next = { ...prev };
          delete next[product.id];
          return next;
        });
      }
    },
    [quantities],
  );

  let body: ReactElement;
  if (state.phase === "loading") {
    body = (
      <Flex justify="center" style={{ padding: 64 }}>
        <Spin size="large" tip="载入商品中…">
          <div style={{ width: 240, height: 80 }} />
        </Spin>
      </Flex>
    );
  } else if (state.phase === "error") {
    body = (
      <Flex vertical align="center" style={{ padding: 64 }}>
        <Alert
          type="error"
          showIcon
          message="获取商品失败"
          description={state.message}
          style={{ maxWidth: 480 }}
        />
        <Button
          type="primary"
          style={{ marginTop: 16 }}
          onClick={() => window.location.reload()}
        >
          重试
        </Button>
      </Flex>
    );
  } else if (state.products.length === 0) {
    body = <Empty description="暂无预售商品" />;
  } else {
    body = (
      <Flex gap="middle" wrap="wrap" justify="center">
        {state.products.map((product) => (
          <ProductCard
            key={product.id}
            product={product}
            quantity={quantities[product.id] ?? 1}
            isPending={pending[product.id] === true}
            orderError={orderErrors[product.id]}
            orderResult={orderResults[product.id]}
            onChangeQuantity={(value) => setQuantity(product.id, value)}
            onOrder={() => void handleOrder(product)}
          />
        ))}
      </Flex>
    );
  }

  return (
    <ConfigProvider
      locale={zhCN}
      theme={{
        algorithm: theme.defaultAlgorithm,
        token: { colorPrimary: "#1769e0", borderRadius: 8 },
      }}
    >
      <main className="page">
        <header className="banner">
          <Typography.Title level={2} style={{ margin: 0 }}>
            SaleSipeed 硬件预售
          </Typography.Title>
          <Typography.Text type="secondary">
            选择商品与数量,点击「下单」即提交预售订单(价格以后端计算为准)。
          </Typography.Text>
        </header>
        {returnBanner ? (
          <Alert
            type={returnBanner.includes("已接受付款") ? "success" : "info"}
            showIcon
            message="Stripe 返回结果(仅展示)"
            description={returnBanner}
            closable
            onClose={() => setReturnBanner(null)}
            style={{ marginBottom: 16 }}
          />
        ) : null}
        {body}
      </main>
    </ConfigProvider>
  );
}

interface ProductCardProps {
  product: Product;
  quantity: number;
  isPending: boolean;
  orderError?: string;
  orderResult?: CreateOrderResponse;
  onChangeQuantity: (value: number | null) => void;
  onOrder: () => void;
}

function ProductCard(props: ProductCardProps): ReactElement {
  const { product, quantity, isPending, orderError, orderResult } = props;
  const soldOut = product.stock <= 0;

  return (
    <Card
      hoverable
      className="product-card"
      cover={
        product.image_url ? (
          <img
            className="card-image"
            src={product.image_url}
            alt={product.name}
          />
        ) : (
          <div className="card-image-placeholder" aria-hidden="true">
            {product.name.slice(0, 2)}
          </div>
        )
      }
      actions={[
        <InputNumber
          key="quantity"
          min={1}
          max={Math.max(1, product.stock)}
          value={quantity}
          disabled={soldOut || isPending}
          onChange={props.onChangeQuantity}
          aria-label="数量"
        />,
        <Button
          key="order"
          type="primary"
          loading={isPending}
          disabled={soldOut}
          onClick={props.onOrder}
        >
          {soldOut ? "不可下单" : "下单"}
        </Button>,
      ]}
    >
      <Card.Meta
        title={
          <Flex align="center" gap="small">
            {product.name}
            <Tag color={soldOut ? "red" : "green"}>
              {soldOut ? "缺货" : `库存 ${product.stock}`}
            </Tag>
          </Flex>
        }
        description={product.description}
      />
      <Typography.Title level={4} className="card-price">
        {formatPrice(product.price, product.currency)}
      </Typography.Title>

      {orderError ? (
        <Alert
          type="error"
          showIcon
          message="下单失败"
          description={orderError}
          style={{ marginTop: 12 }}
        />
      ) : null}
      {orderResult ? (
        <Alert
          type="success"
          showIcon
          message={`下单成功:订单 #${orderResult.id}`}
          description={`total ${formatPrice(orderResult.total_price, orderResult.currency)},状态:${orderResult.status}`}
          style={{ marginTop: 12 }}
        />
      ) : null}
    </Card>
  );
}

export default App;
