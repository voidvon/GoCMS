// Package sitecontacts defines the public contact cards managed by each site.
package sitecontacts

import (
	"encoding/json"
	"fmt"
	"net/mail"
	"net/url"
	"regexp"
	"strings"
	"unicode"
)

const SettingKey = "contact_cards"

var (
	cardID = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,80}$`)
	phone  = regexp.MustCompile(`^\+?[0-9][0-9 .()\-]{2,39}$`)
)

type Channel struct {
	Label string `json:"label"`
	URL   string `json:"url"`
	QRURL string `json:"qr_url"`
}

type Card struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Email    string    `json:"email"`
	Phone    string    `json:"phone"`
	Address  string    `json:"address"`
	Channels []Channel `json:"channels"`
}

type Library map[string]Card

func Parse(raw string) ([]Card, error) {
	if raw == "" {
		return []Card{}, nil
	}
	var cards []Card
	if err := json.Unmarshal([]byte(raw), &cards); err != nil || cards == nil {
		return nil, fmt.Errorf("联系方式格式无效")
	}
	return cards, nil
}

func NewLibrary(cards []Card) (Library, error) {
	if len(cards) > 100 {
		return nil, fmt.Errorf("最多可配置 100 张联系卡片")
	}
	library := make(Library, len(cards))
	for i := range cards {
		card := &cards[i]
		card.Name = strings.TrimSpace(card.Name)
		card.Email = strings.TrimSpace(card.Email)
		card.Phone = strings.TrimSpace(card.Phone)
		card.Address = strings.TrimSpace(card.Address)
		if !cardID.MatchString(card.ID) || library[card.ID].ID != "" {
			return nil, fmt.Errorf("第 %d 张联系卡片的标识无效或重复", i+1)
		}
		if card.Name == "" {
			return nil, fmt.Errorf("第 %d 张联系卡片需要填写名称", i+1)
		}
		if card.Email != "" {
			address, err := mail.ParseAddress(card.Email)
			// A plain address also serves as the mailto target.
			if err != nil || address.Name != "" || address.Address != card.Email || strings.ContainsAny(card.Email, "?#%") {
				return nil, fmt.Errorf("「%s」的邮箱地址无效", card.Name)
			}
		}
		if card.Phone != "" && !phone.MatchString(card.Phone) {
			return nil, fmt.Errorf("「%s」的电话号码无效", card.Name)
		}
		if len(card.Channels) > 20 {
			return nil, fmt.Errorf("「%s」最多可配置 20 个联络渠道", card.Name)
		}
		for j := range card.Channels {
			channel := &card.Channels[j]
			channel.Label = strings.TrimSpace(channel.Label)
			channel.URL = strings.TrimSpace(channel.URL)
			channel.QRURL = strings.TrimSpace(channel.QRURL)
			if channel.Label == "" || (channel.URL == "" && channel.QRURL == "") {
				return nil, fmt.Errorf("「%s」的联络渠道需要名称和链接或二维码", card.Name)
			}
			if (channel.URL != "" && !validURL(channel.URL, false)) || (channel.QRURL != "" && !validURL(channel.QRURL, true)) {
				return nil, fmt.Errorf("「%s」的联络链接或二维码地址无效", card.Name)
			}
		}
		library[card.ID] = *card
	}
	return library, nil
}

func validURL(raw string, allowRelative bool) bool {
	if strings.Contains(raw, "\\") || strings.IndexFunc(raw, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsControl(r)
	}) >= 0 {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u.User != nil {
		return false
	}
	if u.IsAbs() {
		return (u.Scheme == "https" || u.Scheme == "http") && u.Hostname() != ""
	}
	return allowRelative && u.Host == "" && strings.HasPrefix(raw, "/") && !strings.HasPrefix(raw, "//")
}

func ParseSelection(raw string) ([]string, error) {
	if raw == "" {
		return []string{}, nil
	}
	var ids []string
	if err := json.Unmarshal([]byte(raw), &ids); err != nil || ids == nil {
		return nil, fmt.Errorf("联系方式选择格式无效")
	}
	return ids, nil
}

func (library Library) ValidateSelection(ids []string) error {
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if _, exists := library[id]; !exists {
			return fmt.Errorf("选中的联系卡片已不存在，请刷新后重新选择")
		}
		if seen[id] {
			return fmt.Errorf("同一张联系卡片只能选择一次")
		}
		seen[id] = true
	}
	return nil
}

// Resolve keeps the selected order. An empty list inherits the fallback list.
func (library Library) Resolve(selected, fallback []string) []Card {
	if len(selected) == 0 {
		selected = fallback
	}
	cards := make([]Card, 0, len(selected))
	for _, id := range selected {
		if card, exists := library[id]; exists {
			cards = append(cards, card)
		}
	}
	return cards
}
