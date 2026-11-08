package commerce

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type DimensionRecord struct {
	Dimension
	Version      int64 `json:"version"`
	ProductCount int   `json:"product_count"`
}

type dimensionReader interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func dimensionIdentity(d Dimension) string {
	raw, _ := json.Marshal([]string{strings.ToLower(d.Name), d.Type, d.Unit})
	return string(raw)
}

func readDimension(ctx context.Context, reader dimensionReader, site int64, key string) (DimensionRecord, error) {
	var r DimensionRecord
	var raw string
	err := reader.QueryRowContext(ctx, `SELECT definition_json,version FROM gocms_commerce_dimension WHERE site_id=? AND dimension_key=?`, site, key).Scan(&raw, &r.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return r, ErrNotFound
	}
	if err != nil {
		return r, err
	}
	err = json.Unmarshal([]byte(raw), &r.Dimension)
	return r, err
}

func dimensionUsage(ctx context.Context, reader dimensionReader, site int64, key string) (int, error) {
	var count int
	err := reader.QueryRowContext(ctx, `SELECT COUNT(*) FROM gocms_commerce_product p WHERE p.site_id=? AND EXISTS(SELECT 1 FROM json_each(p.dimensions_json) d WHERE json_extract(d.value,'$.key')=?)`, site, key).Scan(&count)
	return count, err
}

func (s Service) Dimensions(ctx context.Context, site int64) ([]DimensionRecord, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT d.definition_json,d.version,(SELECT COUNT(*) FROM gocms_commerce_product p WHERE p.site_id=d.site_id AND EXISTS(SELECT 1 FROM json_each(p.dimensions_json) j WHERE json_extract(j.value,'$.key')=d.dimension_key)) FROM gocms_commerce_dimension d WHERE d.site_id=? ORDER BY d.name_key,d.dimension_key`, site)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []DimensionRecord{}
	for rows.Next() {
		var r DimensionRecord
		var raw string
		if err := rows.Scan(&raw, &r.Version, &r.ProductCount); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(raw), &r.Dimension); err != nil {
			return nil, err
		}
		items = append(items, r)
	}
	return items, rows.Err()
}

type dimensionProduct struct {
	Site       int64
	ID         int64
	Dimensions []Dimension
}

func dimensionProducts(ctx context.Context, tx *sql.Tx, site int64, key string) ([]dimensionProduct, error) {
	rows, err := tx.QueryContext(ctx, `SELECT p.site_id,p.content_id,p.dimensions_json FROM gocms_commerce_product p WHERE p.site_id=? AND EXISTS(SELECT 1 FROM json_each(p.dimensions_json) d WHERE json_extract(d.value,'$.key')=?) ORDER BY p.content_id`, site, key)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	products := []dimensionProduct{}
	for rows.Next() {
		var p dimensionProduct
		var raw string
		if err := rows.Scan(&p.Site, &p.ID, &raw); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(raw), &p.Dimensions); err != nil {
			return nil, err
		}
		products = append(products, p)
	}
	return products, rows.Err()
}

func dimensionVariants(ctx context.Context, tx *sql.Tx, site, content int64) ([]Variant, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id,specifications_json FROM gocms_commerce_offer WHERE site_id=? AND content_id=? AND archived=0 ORDER BY id`, site, content)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	variants := []Variant{}
	for rows.Next() {
		var v Variant
		var raw string
		if err := rows.Scan(&v.ID, &raw); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(raw), &v.Specifications); err != nil {
			return nil, err
		}
		variants = append(variants, v)
	}
	return variants, rows.Err()
}

// Propagation edits only definitions and current SKU parameter labels. Prices,
// SKU IDs, enabled flags, archived rows and historical snapshots stay intact.
func replaceProductDimension(ctx context.Context, tx *sql.Tx, p *dimensionProduct, from, to Dimension) error {
	for _, d := range p.Dimensions {
		if d.Key != from.Key && strings.EqualFold(d.Name, to.Name) {
			return fmt.Errorf("内容 %d 已使用同名维度，请先调整该内容的维度选择: %w", p.ID, ErrInvalid)
		}
	}
	variants, err := dimensionVariants(ctx, tx, p.Site, p.ID)
	if err != nil {
		return err
	}
	for _, v := range variants {
		changed := false
		for i := range v.Specifications {
			spec := &v.Specifications[i]
			if !strings.EqualFold(spec.Name, from.Name) {
				continue
			}
			if to.Type == "select" && !containsOption(to.Options, spec.Value) {
				return fmt.Errorf("候选值“%s”仍被内容 %d 的规格使用，请先调整或移除相应规格行: %w", spec.Value, p.ID, ErrInvalid)
			}
			spec.Name, spec.Unit = to.Name, to.Unit
			changed = true
		}
		if changed {
			raw, _ := json.Marshal(v.Specifications)
			if _, err := tx.ExecContext(ctx, `UPDATE gocms_commerce_offer SET specifications_json=? WHERE site_id=? AND content_id=? AND id=? AND archived=0`, string(raw), p.Site, p.ID, v.ID); err != nil {
				return err
			}
		}
	}
	for i, d := range p.Dimensions {
		if d.Key == from.Key {
			p.Dimensions[i] = to
		}
	}
	raw, _ := json.Marshal(p.Dimensions)
	_, err = tx.ExecContext(ctx, `UPDATE gocms_commerce_product SET dimensions_json=? WHERE site_id=? AND content_id=?`, string(raw), p.Site, p.ID)
	return err
}

func storeDimension(ctx context.Context, tx *sql.Tx, site int64, d Dimension, version int64) error {
	raw, err := json.Marshal(d)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO gocms_commerce_dimension(site_id,dimension_key,identity_key,name_key,definition_json,version) VALUES(?,?,?,?,?,?) ON CONFLICT(site_id,dimension_key) DO UPDATE SET identity_key=excluded.identity_key,name_key=excluded.name_key,definition_json=excluded.definition_json,version=excluded.version`, site, d.Key, dimensionIdentity(d), strings.ToLower(d.Name), string(raw), version)
	return err
}

func (s Service) SaveDimension(ctx context.Context, site int64, key string, input DimensionRecord) (DimensionRecord, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return input, err
	}
	defer tx.Rollback()
	input.ProductCount = 0
	input.Key = key
	if key == "" {
		input.Key, err = randomID("dim_")
		if err != nil {
			return input, err
		}
	}
	definitions := []Dimension{input.Dimension}
	if err := normalizeDimensions(definitions); err != nil {
		return input, err
	}
	input.Dimension = definitions[0]
	var duplicate string
	err = tx.QueryRowContext(ctx, `SELECT dimension_key FROM gocms_commerce_dimension WHERE site_id=? AND identity_key=? AND dimension_key<>?`, site, dimensionIdentity(input.Dimension), input.Key).Scan(&duplicate)
	if err == nil {
		return input, fmt.Errorf("相同名称、类型和单位的维度已存在，请直接选用: %w", ErrInvalid)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return input, err
	}
	version := int64(1)
	if key != "" {
		current, err := readDimension(ctx, tx, site, key)
		if err != nil {
			return input, err
		}
		if current.Version != input.Version {
			return input, fmt.Errorf("维度已被其他管理员修改，请刷新后重新编辑: %w", ErrConflict)
		}
		products, err := dimensionProducts(ctx, tx, site, key)
		if err != nil {
			return input, err
		}
		input.ProductCount = len(products)
		if len(products) > 0 && (current.Type != input.Type || current.Unit != input.Unit) {
			return input, fmt.Errorf("已被内容使用的维度不能修改类型或单位，请新建维度后按产品调整: %w", ErrInvalid)
		}
		for i := range products {
			if err := replaceProductDimension(ctx, tx, &products[i], current.Dimension, input.Dimension); err != nil {
				return input, err
			}
		}
		version = current.Version + 1
	}
	if err := storeDimension(ctx, tx, site, input.Dimension, version); err != nil {
		return input, err
	}
	input.Version = version
	return input, tx.Commit()
}

func (s Service) DeleteDimension(ctx context.Context, site int64, key string, version int64) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	current, err := readDimension(ctx, tx, site, key)
	if err != nil {
		return err
	}
	if current.Version != version {
		return fmt.Errorf("维度已被修改，请刷新后重试: %w", ErrConflict)
	}
	count, err := dimensionUsage(ctx, tx, site, key)
	if err != nil {
		return err
	}
	if count > 0 {
		return fmt.Errorf("维度仍被 %d 条内容使用，请先在内容编辑中取消引用: %w", count, ErrInvalid)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM gocms_commerce_dimension WHERE site_id=? AND dimension_key=?`, site, key); err != nil {
		return err
	}
	return tx.Commit()
}

// Product saves can select definitions but cannot create or mutate the shared
// library. Reject stale labels rather than assigning their prices to new values.
func resolveProductDimensions(ctx context.Context, tx *sql.Tx, site int64, dimensions []Dimension) error {
	for i, d := range dimensions {
		current, err := readDimension(ctx, tx, site, d.Key)
		if errors.Is(err, ErrNotFound) {
			return fmt.Errorf("规格维度不存在，请从规格维度管理中创建后重新选择: %w", ErrInvalid)
		}
		if err != nil {
			return err
		}
		if d.Name != current.Name || d.Type != current.Type || d.Unit != current.Unit {
			return fmt.Errorf("规格维度已更新，请重新打开内容编辑后保存: %w", ErrConflict)
		}
		dimensions[i] = current.Dimension
	}
	return nil
}
