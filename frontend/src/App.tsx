import { useCallback, useEffect, useState } from "react";
import type { ReactElement } from "react";
import {
  Alert,
  Button,
  Card,
  ConfigProvider,
  Descriptions,
  Empty,
  Flex,
  Input,
  InputNumber,
  Modal,
  Spin,
  Tag,
  Typography,
  theme,
} from "antd";
import zhCN from "antd/locale/zh_CN";
import {
  ApiError, cancelOrder, createOrder, fetchProducts, getOrder, refundOrder
} from "./api";
import {
  formatPrice,
  type CreateOrderResponse,
  type OrderView,
  type Product,
} from "./types";

/** UUIDv7 订单号格式校验(查询入口的简单防护,后端仍做权威校验) */
const ORDER_ID_RE =
  /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i;

/** 订单状态 → antd Tag 颜色 */
const STATUS_COLORS: Record<string, string> = {
  PENDING: "gold",
  PAID: "green",
  FAILED: "red",
  CANCELED: "default",
  REFUNDED: "purple",
};

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

  // 回跳携带的订单号预填到「订单查询」输入框
  const [lookupPrefill, setLookupPreFill] = useState<string>("");
  useEffect(() => {
    const params = new URLSearchParams(window.location.search);
    const orderId = params.get("order_id");
    if (orderId) {
      setLookupPreFill(orderId);
    }
  }, []);

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
        <OrderLookupSection prefill={lookupPrefill} />
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

interface OrderLookupSectionProps {
  /** Stripe 回跳携带的订单号(预填输入框) */
  prefill: string;
}

/**
 * 订单号查询区块:输入 UUIDv7 订单号 → 查询成功后弹出 Modal 展示购买内容;
 * Modal 内按状态出示操作按钮:
 *   PAID    → 「申请退款」(调用 /orders/{id}/refund,后端幂等)
 *   PENDING → 「取消订单」(调用 /orders/{id}/cancel,后端归还库存)
 * 其他状态仅展示(操作规则见 README)。
 */
function OrderLookupSection(props: OrderLookupSectionProps): ReactElement {
  const [cancelModal, cancelModalHolder] = Modal.useModal();
  const [input, setInput] = useState(props.prefill);
  const [order, setOrder] = useState<OrderView | null>(null);
  const [open, setOpen] = useState(false);
  const [loading, setLoading] = useState(false);
  const [refunding, setRefunding] = useState(false);
  const [canceling, setCanceling] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);

  useEffect(() => {
    if (props.prefill !== input) {
      setInput(props.prefill);
    }
    // 仅从回跳预填,不自动查询(避免刷新期反复请求)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [props.prefill]);

  const handleLookup = useCallback(async () => {
    const id = input.trim();
    setError(null);
    setNotice(null);
    if (!ORDER_ID_RE.test(id)) {
      setError("请输入合法订单号(UUIDv7 格式,32 位 hex + 4 个连字符)");
      return;
    }
    setLoading(true);
    try {
      const view = await getOrder(id);
      setOrder(view);
      setOpen(true);
      setNotice(null);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "查询失败(未到达后端)");
    } finally {
      setLoading(false);
    }
  }, [input]);

  const handleRefund = useCallback(async () => {
    if (!order) return;
    setRefunding(true);
    setError(null);
    setNotice(null);
    try {
      // 后端返回退款后的最新视图(REFUNDED),直接刷新弹窗展示
      setOrder(await refundOrder(order.id));
      setNotice("退款已受理,状态已更新为 REFUNDED。");
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "退款请求未到达后端");
    } finally {
      setRefunding(false);
    }
  }, [order]);

  const handleCancel = useCallback(async () => {
    if (!order) return;
    cancelModal.confirm({
      title: "确认取消该订单?",
      content: `订单 ${order.id}(PENDING)取消后不可恢复,库存将归还商品。`,
      okButtonProps: { danger: true },
      okText: "取消订单",
      cancelText: "返回",
      onOk: async () => {
        setCanceling(true);
        setError(null);
        setNotice(null);
        try {
          setOrder(await cancelOrder(order.id));
          setNotice("订单已取消,状态更新为 CANCELED,库存已归还。");
        } catch (err) {
          setError(err instanceof ApiError ? err.message : "取消请求未到达后端");
        } finally {
          setCanceling(false);
        }
      },
    });
  }, [order]);

  return (
    <Card size="small" style={{ marginBottom: 24 }} title="订单查询">
      {cancelModalHolder}
      <Flex gap="small" wrap="wrap">
        <Input
          style={{ maxWidth: 420, fontFamily: "monospace" }}
          placeholder="粘贴订单号(UUIDv7),例如 01a1…-…"
          value={input}
          onChange={(e) => setInput(e.target.value)}
          onPressEnter={() => void handleLookup()}
          allowClear
        />
        <Button type="primary" loading={loading} onClick={() => void handleLookup()}>
          查询
        </Button>
      </Flex>

      {error ? (
        <Alert
          type="error"
          showIcon
          message={error}
          style={{ marginTop: 12 }}
        />
      ) : null}

      <Modal
        title="订单详情"
        open={open}
        onCancel={() => setOpen(false)}
        footer={[
          order && order.status === "PAID" ? (
            <Button
              key="refund"
              danger
              loading={refunding}
              onClick={() => void handleRefund()}
            >
              申请退款
            </Button>
          ) : null,
          order && order.status === "PENDING" ? (
            <Button
              key="cancel"
              danger
              loading={canceling}
              onClick={() => void handleCancel()}
            >
              取消订单
            </Button>
          ) : null,
          <Button key="close" type="primary" onClick={() => setOpen(false)}>
            关闭
          </Button>,
        ]}
      >
        {notice ? (
          <Alert
            type="success"
            showIcon
            message={notice}
            style={{ marginBottom: 12 }}
          />
        ) : null}
        {error && open ? (
          <Alert
            type="error"
            showIcon
            message={error}
            style={{ marginBottom: 12 }}
          />
        ) : null}
        {order === null ? null : (
          <Descriptions
            size="small"
            column={1}
            items={[
              {
                key: "id",
                label: "订单号",
                children: (
                  <Typography.Text copyable style={{ fontFamily: "monospace" }}>
                    {order.id}
                  </Typography.Text>
                ),
              },
              {
                key: "status",
                label: "状态",
                children: (
                  <Tag color={STATUS_COLORS[order.status] ?? "default"}>
                    {order.status}
                  </Tag>
                ),
              },
              {
                key: "product",
                label: "购买内容",
                children: `${order.product_name} × ${order.quantity}`,
              },
              {
                key: "total",
                label: "金额",
                children: formatPrice(order.total_price, order.currency),
              },
              {
                key: "created",
                label: "下单时间",
                children: new Date(order.created_at).toLocaleString("zh-CN"),
              },
            ]}
          />
        )}
      </Modal>
    </Card>
  );
}
