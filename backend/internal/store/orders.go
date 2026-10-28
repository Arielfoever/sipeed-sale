package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

var (
	// ErrProductNotFound 商品不存在
	ErrProductNotFound = errors.New("product not found")
	// ErrInsufficientStock 库存不足以覆盖本次购买数量
	ErrInsufficientStock = errors.New("insufficient stock")
	// ErrOrderNotFound 订单不存在
	ErrOrderNotFound = errors.New("order not found")
)

// Order 对应数据库 orders 表。
type Order struct {
	ID                    uuid.UUID // 订单号(UUIDv7,应用层生成)
	ProductID             uuid.UUID
	Quantity              int64
	UnitPrice             int64 // 分
	TotalPrice            int64 // 分
	Currency              string
	Status                string // PENDING / PAID / FAILED / CANCELED / REFUNDED
	RequestID             *uuid.UUID
	StripeSessionID       *string // Checkout Session(第 3 步)
	StripeSessionURL      *string // Checkout 付款页 URL(session 仅创建时返回一次)
	StripePaymentIntentID *string // PaymentIntent(webhook 推进时记录,退款用)
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

// CreateOrderInput 下单入参。
type CreateOrderInput struct {
	ProductID uuid.UUID
	Quantity  int64
	// RequestID 客户端随机生成的请求 ID(X-Request-Id);同 ID 重复提交
	// 直接返回既有订单,不重复入库(幂等)。
	RequestID *uuid.UUID
}

// CreateOrder 在单个事务内完成:
//  1. SELECT ... FOR UPDATE 锁定商品行,校验库存;
//  2. 计算 total_price 单位"分"(unit_price * quantity);
//  3. 生成 UUIDv7 订单号并插入 PENDING 订单,request_id 冲突则复用已存在
//     订单(幂等);
//  4. 扣减库存。
//
// 金额一律由服务端按商品表价格计算,不受入参影响。
func (s *Store) CreateOrder(ctx context.Context, in CreateOrderInput) (Order, error) {
	tx, err := s.pool.BeginTx(ctx, txDefaults())
	if err != nil {
		return Order{}, err
	}
	defer rollback(ctx, tx)

	// 1) 行锁 + 读取价格/库存(手写 SQL,行级锁保证并发扣减安全)
	var unitPrice, stock int64
	var currency string
	err = tx.QueryRow(ctx, `
		SELECT price, currency, stock
		FROM products
		WHERE id = $1
		FOR UPDATE
	`, in.ProductID).Scan(&unitPrice, &currency, &stock)
	if isNoRows(err) {
		return Order{}, ErrProductNotFound
	}
	if err != nil {
		return Order{}, err
	}

	if in.Quantity < 1 || stock < in.Quantity {
		return Order{}, ErrInsufficientStock
	}

	total := unitPrice * in.Quantity
	orderID, err := uuid.NewV7()
	if err != nil {
		return Order{}, fmt.Errorf("generate order id (uuidv7): %w", err)
	}

	// 2) 插入订单;同 request_id 重复提交时不插(幂等)
	var createdAt, updatedAt time.Time
	var status string
	err = tx.QueryRow(ctx, `
		INSERT INTO orders
			(id, product_id, quantity, unit_price, total_price, currency, status, request_id)
		VALUES ($1, $2, $3, $4, $5, $6, 'PENDING', $7)
		RETURNING created_at, updated_at, status
	`,
		orderID, in.ProductID, in.Quantity, unitPrice, total, currency, in.RequestID,
	).Scan(&createdAt, &updatedAt, &status)
	if err != nil && isUniqueRequestID(err) {
		// 请求 ID 已存在 → 复用既有订单(幂等),且不重复扣库存
		order, err := s.getOrderByRequestID(ctx, tx, in.RequestID)
		if err != nil {
			return Order{}, err
		}
		if err := commit(ctx, tx); err != nil {
			return Order{}, err
		}
		return order, nil
	}
	if err != nil {
		return Order{}, err
	}

	// 3) 扣减库存(商品行已被 FOR UPDATE 锁定,此时必成功)
	cmd, err := tx.Exec(ctx, `
		UPDATE products
		SET stock = stock - $2
		WHERE id = $1
	`, in.ProductID, in.Quantity)
	if err != nil {
		return Order{}, err
	}
	if cmd.RowsAffected() != 1 {
		return Order{}, errors.New("stock update affected unexpected row count")
	}

	if err := commit(ctx, tx); err != nil {
		return Order{}, err
	}

	return Order{
		ID:         orderID,
		ProductID:  in.ProductID,
		Quantity:   in.Quantity,
		UnitPrice:  unitPrice,
		TotalPrice: total,
		Currency:   currency,
		Status:     status,
		RequestID:  in.RequestID,
		CreatedAt:  createdAt,
		UpdatedAt:  updatedAt,
	}, nil
}

// MarkOrderStatusBySession 由 Stripe(webhook)把 PENDING 订单推进到目标
// 状态,幂等核心:仅当旧状态为 PENDING 时更新,重复投递命中 0 行。
// paymentIntent 可选:支付成功时记录 PaymentIntent(退款用)。
// 返回受影响行数:1 = 首次确认,0 = 重复投递 / 订单不存在 / 状态不符。
func (s *Store) MarkOrderStatusBySession(
	ctx context.Context,
	stripeSessionID string,
	newStatus string,
	paymentIntent *string,
) (int64, error) {
	cmd, err := s.pool.Exec(ctx, `
		UPDATE orders
		SET status = $2,
		    stripe_payment_intent_id = COALESCE($3, stripe_payment_intent_id),
		    updated_at = now()
		WHERE stripe_session_id = $1 AND status = 'PENDING'
	`, stripeSessionID, newStatus, paymentIntent)
	if err != nil {
		return 0, err
	}
	return cmd.RowsAffected(), nil
}

// GetOrder 读取订单(按 ID)。
func (s *Store) GetOrder(ctx context.Context, id uuid.UUID) (Order, error) {
	o, err := scanOrderRow(
		s.pool.QueryRow(ctx, orderColumnsSQL+` FROM orders WHERE id = $1`, id),
	)
	if isNoRows(err) {
		return Order{}, ErrOrderNotFound
	}
	return o, err
}

func (s *Store) getOrderByRequestID(
	ctx context.Context,
	tx Querier,
	requestID *uuid.UUID,
) (Order, error) {
	if requestID == nil {
		return Order{}, errors.New("conflict on nil request_id")
	}
	o, err := scanOrderRow(
		tx.QueryRow(ctx, orderColumnsSQL+` FROM orders WHERE request_id = $1`, *requestID),
	)
	if err != nil {
		return Order{}, err
	}
	return o, nil
}

const orderColumnsSQL = `
	SELECT id, product_id, quantity, unit_price, total_price, currency, status,
	       request_id, stripe_session_id, stripe_session_url,
	       stripe_payment_intent_id, created_at, updated_at
`

func scanOrderRow(rows pgxRow) (Order, error) {
	var o Order
	err := rows.Scan(
		&o.ID, &o.ProductID, &o.Quantity, &o.UnitPrice, &o.TotalPrice,
		&o.Currency, &o.Status, &o.RequestID, &o.StripeSessionID,
		&o.StripeSessionURL, &o.StripePaymentIntentID, &o.CreatedAt, &o.UpdatedAt,
	)
	if err != nil {
		return Order{}, err
	}
	return o, nil
}

// AttachStripeSession 把（一次性创建的）Stripe Checkout Session 绑定到
// PENDING 订单上;(session id / url 存库,重复 CREATE 直接复用,幂等)
func (s *Store) AttachStripeSession(
	ctx context.Context,
	orderID uuid.UUID,
	sessionID string,
	sessionURL string,
) error {
	cmd, err := s.pool.Exec(ctx, `
		UPDATE orders
		SET stripe_session_id = $2, stripe_session_url = $3, updated_at = now()
		WHERE id = $1 AND status = 'PENDING'
	`, orderID, sessionID, sessionURL)
	if err != nil {
		return err
	}
	if cmd.RowsAffected() != 1 {
		return ErrOrderNotFound
	}
	return nil
}
