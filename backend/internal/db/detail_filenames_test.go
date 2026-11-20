package db

import (
	"context"
	"testing"

	"gocms/internal/routing"
)

func TestMigrateDetailFilenamesPreservesURLsAndIsIdempotent(t *testing.T) {
	ctx := context.Background()
	database, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	for _, ensure := range []func() error{
		func() error { return EnsureUnifiedCategories(ctx, database) },
		func() error { return EnsureModels(ctx, database) },
		func() error { return EnsureContent(ctx, database) },
	} {
		if err := ensure(); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := database.Exec(`
		INSERT INTO gocms_category (id, name, detail_path, detail_file_pattern) VALUES
			(1, 'News', 'news', '{id}/index.html'),
			(2, 'Events', 'events', 'archives/{id}.html'),
			(3, 'Notes', 'notes', '{id}');
		INSERT INTO gocms_content (id, category_id, route_key, title) VALUES
			(532, 1, 'steam-system-critical-spares', 'News'),
			(533, 2, 'summer', 'Events'),
			(534, 3, 'summary', 'Notes');`); err != nil {
		t.Fatal(err)
	}
	want := []struct {
		id                 int64
		dir, filename, url string
	}{
		{532, "news", "steam-system-critical-spares/index.html", "/news/steam-system-critical-spares/"},
		{533, "events", "archives/summer.html", "/events/archives/summer.html"},
		{534, "notes", "summary", "/notes/summary"},
	}
	for attempt := 0; attempt < 2; attempt++ {
		if err := CreateSchema(ctx, database); err != nil {
			t.Fatal(err)
		}
		for _, record := range want {
			var filename string
			if err := database.QueryRow(`SELECT route_key FROM gocms_content WHERE id = ?`, record.id).Scan(&filename); err != nil {
				t.Fatal(err)
			}
			if filename != record.filename || routing.DetailURL(record.dir, filename) != record.url {
				t.Fatalf("attempt %d, record %d: got %q, want %q with URL %q", attempt+1, record.id, filename, record.filename, record.url)
			}
		}
	}
}
