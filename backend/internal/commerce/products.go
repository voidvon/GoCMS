package commerce

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"gocms/internal/payment"
)

type Specification struct {
	Name  string `json:"name"`
	Value string `json:"value"`
	Unit  string `json:"unit,omitempty"`
}

type Variant struct {
	FixedPrices    map[string]int64 `json:"fixed_prices,omitempty"`
	ID             int64            `json:"id"`
	SKU            string           `json:"sku"`
	Specifications []Specification  `json:"specifications"`
	Amount         int64            `json:"amount"`
	Enabled        bool             `json:"enabled"`
}

type Product struct {
	Enabled    bool        `json:"enabled"`
	Kind       string      `json:"kind"`
	Currency   string      `json:"currency"`
	Dimensions []Dimension `json:"dimensions"`
	Variants   []Variant   `json:"variants"`
}

const publicOfferColumns = `f.id,c.title,p.kind,f.amount,p.currency,c.cover_image,c.id,f.enabled,f.sku,f.specifications_json,f.fixed_prices_json`
const availableOfferFrom = ` FROM gocms_commerce_offer f
 JOIN gocms_commerce_product p ON p.content_id=f.content_id AND p.site_id=f.site_id
 JOIN gocms_content c ON c.id=p.content_id AND c.site_id=p.site_id
 JOIN gocms_category category ON category.id=c.category_id AND category.site_id=c.site_id`
const availableOfferWhere = `f.site_id=? AND f.archived=0 AND f.enabled=1 AND p.enabled=1 AND c.visible=1 AND category.commerce_enabled=1 AND category.page_type<>'link'`

func scanOffer(row scanner) (Offer, error) {
	var o Offer
	var specifications, fixed string
	err := row.Scan(&o.ID, &o.Name, &o.Kind, &o.Amount, &o.Currency, &o.ImageURL, &o.ContentID, &o.Enabled, &o.SKU, &specifications, &fixed)
	if err != nil {
		return o, err
	}
	if err = json.Unmarshal([]byte(specifications), &o.Specifications); err != nil {
		return o, err
	}
	err = json.Unmarshal([]byte(fixed), &o.FixedPrices)
	return o, err
}

// Product loads the content's trading configuration and selected dimensions.
func (s Service) Product(ctx context.Context, site, content int64) (*Product, error) {
	p := Product{Dimensions: []Dimension{}, Variants: []Variant{}}
	var dimensions string
	err := s.DB.QueryRowContext(ctx, `SELECT enabled,kind,currency,dimensions_json FROM gocms_commerce_product WHERE site_id=? AND content_id=?`, site, content).Scan(&p.Enabled, &p.Kind, &p.Currency, &dimensions)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(dimensions), &p.Dimensions); err != nil {
		return nil, err
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT id,sku,specifications_json,amount,enabled,fixed_prices_json FROM gocms_commerce_offer WHERE site_id=? AND content_id=? AND archived=0 ORDER BY id`, site, content)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var v Variant
		var raw, fixed string
		if err := rows.Scan(&v.ID, &v.SKU, &raw, &v.Amount, &v.Enabled, &fixed); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(raw), &v.Specifications); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(fixed), &v.FixedPrices); err != nil {
			return nil, err
		}
		p.Variants = append(p.Variants, v)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return &p, nil
}

// SaveProduct participates in the CMS content transaction. Variant IDs are
// stable, removed variants are archived, and order snapshots are never edited.
func (s Service) SaveProduct(ctx context.Context, tx *sql.Tx, site, content int64, p Product) error {
	if p.Kind != "physical" && p.Kind != "digital" && p.Kind != "service" {
		return fmt.Errorf("请选择有效的商品或服务类型: %w", ErrInvalid)
	}
	if _, err := payment.Exponent(p.Currency); err != nil {
		return fmt.Errorf("请选择支持的币种: %w", ErrInvalid)
	}
	if len(p.Variants) > MaxProductVariants || (p.Enabled && len(p.Variants) == 0) {
		return fmt.Errorf("启用销售需至少一个规格，每条内容最多 %d 个规格: %w", MaxProductVariants, ErrInvalid)
	}
	if p.Dimensions == nil {
		p.Dimensions = []Dimension{}
	}
	if err := normalizeDimensions(p.Dimensions); err != nil {
		return err
	}
	if err := resolveProductDimensions(ctx, tx, site, p.Dimensions); err != nil {
		return err
	}
	var title, image string
	var category int64
	if err := tx.QueryRowContext(ctx, `SELECT title,cover_image,category_id FROM gocms_content WHERE site_id=? AND id=?`, site, content).Scan(&title, &image, &category); err != nil {
		return err
	}
	if p.Enabled {
		valid, err := validOfferImage(ctx, tx, site, image)
		if err != nil {
			return err
		}
		if !valid || len(image) > 255 {
			return fmt.Errorf("请先为内容设置有效的 HTTPS 封面或本站已上传图片: %w", ErrInvalid)
		}
		if !validText(title, 128) {
			return fmt.Errorf("用于销售的内容标题最多 128 字节: %w", ErrInvalid)
		}
		if category <= 0 {
			return fmt.Errorf("请选择所属分类: %w", ErrInvalid)
		}
	}
	ids := map[int64]bool{}
	skus, combinations := map[string]bool{}, map[string]bool{}
	enabled := false
	for i := range p.Variants {
		v := &p.Variants[i]
		if len(v.FixedPrices) > len(SupportedCurrencies()) {
			return ErrInvalid
		}
		for currency, amount := range v.FixedPrices {
			if _, err := payment.FormatAmount(amount, currency); err != nil || amount <= 0 {
				return fmt.Errorf("固定币种售价无效: %w", ErrInvalid)
			}
		}
		if v.ID < 0 || (v.ID > 0 && ids[v.ID]) {
			return ErrInvalid
		}
		if v.ID > 0 {
			var count int
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM gocms_commerce_offer WHERE id=? AND site_id=? AND content_id=?`, v.ID, site, content).Scan(&count); err != nil {
				return err
			}
			if count != 1 {
				return ErrInvalid
			}
			ids[v.ID] = true
		}
		v.SKU = strings.TrimSpace(v.SKU)
		if v.SKU == "" {
			var err error
			v.SKU, err = randomID("sku_")
			if err != nil {
				return err
			}
		}
		if !validText(v.SKU, 64) || skus[strings.ToLower(v.SKU)] {
			return fmt.Errorf("规格编号不能为空或重复，最多 64 字节: %w", ErrInvalid)
		}
		skus[strings.ToLower(v.SKU)] = true
		if _, err := payment.FormatAmount(v.Amount, p.Currency); err != nil {
			return fmt.Errorf("每个规格都需要有效的正数价格: %w", ErrInvalid)
		}
		if v.Amount <= 0 || len(v.Specifications) > MaxProductDimensions {
			return ErrInvalid
		}
		if v.Specifications == nil {
			v.Specifications = []Specification{}
		}
		names := map[string]bool{}
		canonical := []string{}
		for j := range v.Specifications {
			spec := &v.Specifications[j]
			spec.Name = strings.TrimSpace(spec.Name)
			spec.Value = strings.TrimSpace(spec.Value)
			spec.Unit = strings.TrimSpace(spec.Unit)
			name := strings.ToLower(spec.Name)
			if !validText(spec.Name, 40) || !validText(spec.Value, 120) || names[name] || (spec.Unit != "" && !validText(spec.Unit, 20)) {
				return fmt.Errorf("规格参数名称和值不能为空，同一规格内参数名称不能重复: %w", ErrInvalid)
			}
			names[name] = true
			canonical = append(canonical, name+"\x00"+spec.Value)
		}
		if err := validateVariantDimensions(v.Specifications, p.Dimensions); err != nil {
			return fmt.Errorf("规格 %d: %w", i+1, err)
		}
		for j := range v.Specifications {
			for _, d := range p.Dimensions {
				if strings.EqualFold(v.Specifications[j].Name, d.Name) {
					v.Specifications[j].Name = d.Name
					v.Specifications[j].Unit = d.Unit
				}
			}
		}
		sort.Strings(canonical)
		raw, _ := json.Marshal(canonical)
		combination := string(raw)
		if combinations[combination] {
			return fmt.Errorf("同一内容不能配置重复的规格参数组合: %w", ErrInvalid)
		}
		combinations[combination] = true
		enabled = enabled || v.Enabled
	}
	if p.Enabled && !enabled {
		return fmt.Errorf("启用销售需至少启用一个规格: %w", ErrInvalid)
	}
	dimensionsJSON, err := json.Marshal(p.Dimensions)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO gocms_commerce_product(content_id,site_id,enabled,kind,currency,dimensions_json) VALUES(?,?,?,?,?,?) ON CONFLICT(content_id) DO UPDATE SET enabled=excluded.enabled,kind=excluded.kind,currency=excluded.currency,dimensions_json=excluded.dimensions_json WHERE site_id=excluded.site_id`, content, site, p.Enabled, p.Kind, p.Currency, string(dimensionsJSON)); err != nil {
		return err
	}
	// Archive first so replacement rows may reuse an old SKU without deleting it.
	if _, err := tx.ExecContext(ctx, `UPDATE gocms_commerce_offer SET archived=1,enabled=0 WHERE site_id=? AND content_id=?`, site, content); err != nil {
		return err
	}
	for _, v := range p.Variants {
		raw, _ := json.Marshal(v.Specifications)
		fixed := "{}"
		if len(v.FixedPrices) > 0 {
			encoded, _ := json.Marshal(v.FixedPrices)
			fixed = string(encoded)
		}
		if v.ID == 0 {
			if _, err := tx.ExecContext(ctx, `INSERT INTO gocms_commerce_offer(site_id,content_id,name,kind,amount,currency,image_url,enabled,sku,specifications_json,archived,fixed_prices_json) VALUES(?,?,?,?,?,?,?,?,?,?,0,?)`, site, content, title, p.Kind, v.Amount, p.Currency, image, v.Enabled, v.SKU, string(raw), fixed); err != nil {
				return err
			}
		} else {
			if _, err := tx.ExecContext(ctx, `UPDATE gocms_commerce_offer SET name=?,kind=?,amount=?,currency=?,image_url=?,enabled=?,sku=?,specifications_json=?,fixed_prices_json=?,archived=0 WHERE id=? AND site_id=? AND content_id=?`, title, p.Kind, v.Amount, p.Currency, image, v.Enabled, v.SKU, string(raw), fixed, v.ID, site, content); err != nil {
				return err
			}
		}
	}
	return nil
}
