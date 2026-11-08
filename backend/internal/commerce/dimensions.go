package commerce

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

const MaxProductVariants = 500
const MaxProductDimensions = 12
const MaxDimensionOptions = 5000

// Dimension is a site-shared definition selected by products. Product copies
// keep display order; SKU and order snapshots retain name/value/unit pairs.
type Dimension struct {
	Key     string   `json:"key"`
	Name    string   `json:"name"`
	Type    string   `json:"type"`
	Unit    string   `json:"unit"`
	Options []string `json:"options"`
}

var dimensionNumber = regexp.MustCompile(`^[+-]?(?:[0-9]+(?:\.[0-9]+)?|\.[0-9]+)$`)

func containsOption(options []string, value string) bool {
	for _, option := range options {
		if option == value {
			return true
		}
	}
	return false
}

func normalizeDimensions(dimensions []Dimension) error {
	if len(dimensions) > MaxProductDimensions {
		return fmt.Errorf("每条内容最多 %d 个规格维度: %w", MaxProductDimensions, ErrInvalid)
	}
	keys, names := map[string]bool{}, map[string]bool{}
	for i := range dimensions {
		d := &dimensions[i]
		d.Key, d.Name, d.Unit = strings.TrimSpace(d.Key), strings.TrimSpace(d.Name), strings.TrimSpace(d.Unit)
		name := strings.ToLower(d.Name)
		if !validText(d.Key, 64) || !validText(d.Name, 40) || keys[d.Key] || names[name] || len(d.Unit) > 20 || (d.Unit != "" && !validText(d.Unit, 20)) {
			return fmt.Errorf("规格维度名称和标识不能为空或重复，名称最多 40 字节、单位最多 20 字节: %w", ErrInvalid)
		}
		keys[d.Key], names[name] = true, true
		if d.Type != "text" && d.Type != "number" && d.Type != "select" {
			return fmt.Errorf("请选择有效的规格维度类型: %w", ErrInvalid)
		}
		if d.Type != "select" {
			d.Options = []string{}
			continue
		}
		if len(d.Options) == 0 || len(d.Options) > MaxDimensionOptions {
			return fmt.Errorf("选项规格维度需配置 1 至 %d 个候选值: %w", MaxDimensionOptions, ErrInvalid)
		}
		seen := map[string]bool{}
		for j := range d.Options {
			d.Options[j] = strings.TrimSpace(d.Options[j])
			if !validText(d.Options[j], 120) || seen[d.Options[j]] {
				return fmt.Errorf("规格候选值不能为空或重复，最多 120 字节: %w", ErrInvalid)
			}
			seen[d.Options[j]] = true
		}
	}
	return nil
}

func validateVariantDimensions(specifications []Specification, dimensions []Dimension) error {
	if len(specifications) != len(dimensions) {
		return fmt.Errorf("请填写全部规格维度的值: %w", ErrInvalid)
	}
	values := map[string]string{}
	for _, spec := range specifications {
		values[strings.ToLower(spec.Name)] = spec.Value
	}
	for _, d := range dimensions {
		value, found := values[strings.ToLower(d.Name)]
		if !found || value == "" {
			return fmt.Errorf("请填写规格维度“%s”: %w", d.Name, ErrInvalid)
		}
		if d.Type == "select" && !containsOption(d.Options, value) {
			return fmt.Errorf("“%s”的值不在候选值中，请更新选项或修改规格: %w", d.Name, ErrInvalid)
		}
		if d.Type == "number" && !dimensionNumber.MatchString(value) {
			return fmt.Errorf("“%s”需要有效数字: %w", d.Name, ErrInvalid)
		}
	}
	return nil
}

// PublicDimensions is only returned alongside purchasable offers for the
// requested content; it contains no prices, disabled SKU rows or credentials.
func (s Service) PublicDimensions(ctx context.Context, site, content int64) ([]Dimension, error) {
	var raw string
	if err := s.DB.QueryRowContext(ctx, `SELECT dimensions_json FROM gocms_commerce_product WHERE site_id=? AND content_id=?`, site, content).Scan(&raw); err != nil {
		return nil, err
	}
	dimensions := []Dimension{}
	err := json.Unmarshal([]byte(raw), &dimensions)
	return dimensions, err
}
