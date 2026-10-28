DROP INDEX IF EXISTS uq_orders_stripe_payment_intent;
ALTER TABLE orders DROP COLUMN stripe_payment_intent_id;
