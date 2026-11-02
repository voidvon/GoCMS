package generator

import (
	"fmt"

	"gocms/internal/consent"
)

func applyConsent(pages map[string][]byte, policy consent.Policy) error {
	for name, page := range pages {
		if !isHTMLPage(name) {
			continue
		}
		result, err := consent.Process(page, policy)
		if err != nil {
			return fmt.Errorf("生成 %s 的隐私同意组件: %w", name, err)
		}
		pages[name] = result
	}
	return nil
}
