package generator

import (
	"fmt"
	"sort"
	"strings"
)

// contentItems queries the existing publication snapshot, including its language
// projection. Templates receive data and retain ownership of all markup.
func (c *content) contentItems(categoryID, limit int, descendants, featured bool, order string) ([]ListItem, error) {
	return c.contentItemsFiltered(categoryID, limit, descendants, featured, false, order)
}

func (c *content) contentItemsWithImage(categoryID, limit int, descendants, featured, imageOnly bool, order string) ([]ListItem, error) {
	return c.contentItemsFiltered(categoryID, limit, descendants, featured, imageOnly, order)
}

func (c *content) contentItemsFiltered(categoryID, limit int, descendants, featured, imageOnly bool, order string) ([]ListItem, error) {
	if categoryID < 0 || limit < 1 || limit > 500 {
		return nil, fmt.Errorf("contentItems: category must be >= 0 and limit must be 1–500")
	}
	if order != "sort" && order != "newest" && order != "oldest" {
		return nil, fmt.Errorf("contentItems: unsupported order %q", order)
	}
	rows := []Row{}
	for _, row := range c.visibleContents() {
		if categoryID != 0 && row.n("category_id") != categoryID && !(descendants && c.under(row.n("category_id"), categoryID)) {
			continue
		}
		if featured && row.n("featured") != 1 {
			continue
		}
		if imageOnly && strings.TrimSpace(row["cover_image"]) == "" {
			continue
		}
		rows = append(rows, row)
	}
	if order != "sort" {
		sort.SliceStable(rows, func(i, j int) bool {
			if rows[i]["published_at"] == rows[j]["published_at"] {
				if order == "newest" {
					return rows[i].n("id") > rows[j].n("id")
				}
				return rows[i].n("id") < rows[j].n("id")
			}
			if order == "newest" {
				return rows[i]["published_at"] > rows[j]["published_at"]
			}
			return rows[i]["published_at"] < rows[j]["published_at"]
		})
	}
	if len(rows) > limit {
		rows = rows[:limit]
	}
	items := make([]ListItem, 0, len(rows))
	for i, row := range rows {
		items = append(items, c.listItem(row, c.cat(row.n("category_id")), i, len(rows)))
	}
	return items, nil
}
