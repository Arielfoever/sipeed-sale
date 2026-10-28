-- webhook 推进 PAID 时落库 payment_intent(第 5 步退款要用)。
ALTER TABLE orders ADD COLUMN stripe_payment_intent_id TEXT;
CREATE UNIQUE INDEX IF NOT EXISTS uq_orders_stripe_payment_intent
    ON orders (stripe_payment_intent_id) WHERE stripe_payment_intent_id IS NOT NULL;
