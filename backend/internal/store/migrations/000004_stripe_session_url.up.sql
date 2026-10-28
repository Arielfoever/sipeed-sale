-- Checkout Session 的访问地址(session 仅在创建时返回一次,持久化以便
-- 同一订单重复请求时复用同一付款链接)。
ALTER TABLE orders ADD COLUMN stripe_session_url TEXT;
