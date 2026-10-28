package store

import (
	"context"

	"github.com/google/uuid"
)

// Product 对应数据库 products 表(与 OpenAPI 的 Product 一致)。
type Product struct {
	ID          uuid.UUID
	Name        string
	Description string
	Price       int64 // 分
	Currency    string
	Stock       int64
	ImageURL    *string // nil 表示未设置
}

// ListProducts 返回全部预售商品(按名称排序)。
func (s *Store) ListProducts(ctx context.Context) ([]Product, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, name, description, price, currency, stock, image_url
		FROM products
		ORDER BY name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Product
	for rows.Next() {
		var p Product
		if err := rows.Scan(&p.ID, &p.Name, &p.Description, &p.Price, &p.Currency, &p.Stock, &p.ImageURL); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// GetProduct 读取单个商品(下单校验使用)。
func (s *Store) GetProduct(ctx context.Context, id uuid.UUID) (Product, error) {
	var p Product
	err := s.pool.QueryRow(ctx, `
		SELECT id, name, description, price, currency, stock, image_url
		FROM products
		WHERE id = $1
	`, id).Scan(&p.ID, &p.Name, &p.Description, &p.Price, &p.Currency, &p.Stock, &p.ImageURL)
	if err != nil {
		return Product{}, err
	}
	return p, nil
}
