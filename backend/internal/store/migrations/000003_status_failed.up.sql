-- 扩展终态:Stripe 异步支付方式失败时订单标记为 FAILED(webhook 推进)。
ALTER TABLE orders
    DROP CONSTRAINT orders_status_check;
ALTER TABLE orders
    ADD CONSTRAINT orders_status_check
    CHECK (status IN ('PENDING', 'PAID', 'FAILED', 'CANCELED', 'REFUNDED'));
