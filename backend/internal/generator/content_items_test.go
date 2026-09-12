package generator

import "testing"

func TestContentItemsFilteringAndLoopFields(t *testing.T) {
	c := &content{tables: map[string][]Row{
		"gocms_category": {{"id": "71", "name": "任意栏目", "parent_id": "0"}, {"id": "82", "name": "任意子栏目", "parent_id": "71"}},
		"gocms_content": {
			{"id": "1", "category_id": "71", "visible": "1", "published_at": "2026-01-01", "extra_data": `{"material":"<steel>"}`, "cover_image": "/images/one.jpg"},
			{"id": "2", "category_id": "82", "visible": "1", "featured": "1", "published_at": "2026-02-01", "cover_image": "/images/two.jpg"},
			{"id": "3", "category_id": "71", "visible": "0", "published_at": "2026-03-01"},
		},
	}}
	items, err := c.contentItems(71, 10, true, false, "newest")
	if err != nil || len(items) != 2 {
		t.Fatalf("items: %v %v", items, err)
	}
	if items[0].ID != 2 || !items[0].First || items[0].Index != 1 || !items[1].Last || items[1].Fields["material"] != "&lt;steel&gt;" {
		t.Fatalf("loop fields: %+v", items)
	}
	items, err = c.contentItems(71, 10, false, false, "oldest")
	if err != nil || len(items) != 1 || items[0].ID != 1 {
		t.Fatalf("direct category: %+v %v", items, err)
	}
	items, err = c.contentItems(0, 1, true, true, "sort")
	if err != nil || len(items) != 1 || items[0].ID != 2 {
		t.Fatalf("featured: %+v %v", items, err)
	}
	if _, err := c.contentItems(0, 10, false, false, "SQL"); err == nil {
		t.Fatal("invalid sort accepted")
	}
	if _, err := c.contentItems(0, 501, false, false, "sort"); err == nil {
		t.Fatal("invalid limit accepted")
	}
	items, err = c.contentItemsWithImage(71, 10, true, false, true, "newest")
	if err != nil || len(items) != 2 {
		t.Fatalf("image filter: %+v %v", items, err)
	}
}
