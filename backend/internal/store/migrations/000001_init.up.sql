-- 预售站点初始 schema。
-- 金额约定:一律为最小货币单位(分)的 bigint;ID 一律 UUIDv7(应用层生成,
-- 时间有序利于聚簇索引,故用 uuid 主键而不是自增)。

CREATE TABLE products (
    id          uuid PRIMARY KEY,
    name        text  NOT NULL,
    description text  NOT NULL,
    price       bigint NOT NULL CHECK (price >= 0),
    currency    char(3) NOT NULL,
    stock       integer NOT NULL CHECK (stock >= 0),
    image_url   text
);

CREATE TABLE orders (
    id            uuid PRIMARY KEY,              -- 订单号,UUIDv7(应用层生成)
    product_id    uuid NOT NULL REFERENCES products (id),
    quantity      integer NOT NULL CHECK (quantity >= 1),
    unit_price    bigint NOT NULL CHECK (unit_price >= 0),
    total_price   bigint NOT NULL CHECK (total_price >= 0),
    currency      char(3) NOT NULL,
    status        text   NOT NULL DEFAULT 'PENDING'
                  CHECK (status IN ('PENDING', 'PAID', 'CANCELED', 'REFUNDED')),
    -- 幂等:同一次下单请求去重(浏览器防重复点击 / 网络重试)
    request_id    uuid UNIQUE,
    -- 第 3 步 Stripe Checkout 使用:Checkout Session 标识,webhook 幂等确认
    stripe_session_id text UNIQUE,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_orders_product_id  ON orders (product_id);
CREATE INDEX idx_orders_status      ON orders (status) WHERE status = 'PENDING';
