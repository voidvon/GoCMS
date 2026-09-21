package member

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"gocms/internal/db"
)

func TestSessionLimitRotationAndIsolation(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	ctx := context.Background()
	if err := db.CreateSchema(ctx, database); err != nil {
		t.Fatal(err)
	}
	s := Service{DB: database}
	a, err := s.Register(ctx, "alice", "", "password123")
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Register(ctx, "bob", "", "password123")
	if err != nil {
		t.Fatal(err)
	}
	login := func(name, token string) (string, error) {
		_, v, e := s.Login(ctx, name, "password123", "127.0.0.1", "test", token)
		return v, e
	}
	first, err := login("alice", "")
	if err != nil {
		t.Fatal(err)
	}
	second, err := login("alice", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := login("alice", ""); !errors.Is(err, ErrSessionLimit) {
		t.Fatalf("limit: %v", err)
	}
	rotated, err := login("alice", first)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Authenticate(ctx, first); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("old cookie still valid")
	}
	if _, err := s.Authenticate(ctx, rotated); err != nil {
		t.Fatal(err)
	}
	bob, err := login("bob", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := login("alice", bob); !errors.Is(err, ErrSessionLimit) {
		t.Fatal("foreign cookie freed slot")
	}
	if _, err := s.Authenticate(ctx, bob); err != nil {
		t.Fatal("foreign session removed")
	}
	if err := s.Logout(ctx, second); err != nil {
		t.Fatal(err)
	}
	third, err := login("alice", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec("UPDATE gocms_user_session SET expires_at=? WHERE token_hash=?", time.Now().Unix(), TokenHash(third)); err != nil {
		t.Fatal(err)
	}
	if _, err := login("alice", ""); err != nil {
		t.Fatal("expired session counted", err)
	}
	if _, err := database.Exec("UPDATE gocms_user SET max_sessions=1 WHERE id=?", a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := login("alice", rotated); !errors.Is(err, ErrSessionLimit) {
		t.Fatal("lowered limit bypassed")
	}
	if _, err := s.Authenticate(ctx, rotated); err != nil {
		t.Fatal("rejected rotation destroyed session")
	}
}

func TestConcurrentLoginLimit(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	ctx := context.Background()
	if err := db.CreateSchema(ctx, database); err != nil {
		t.Fatal(err)
	}
	s := Service{DB: database}
	u, err := s.Register(ctx, "alice", "", "password123")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec("UPDATE gocms_user SET max_sessions=1 WHERE id=?", u.ID); err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 3)
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, err := s.Login(ctx, "alice", "password123", "127.0.0.1", "test", "")
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		} else if !errors.Is(err, ErrSessionLimit) {
			t.Fatal(err)
		}
	}
	if success != 1 {
		t.Fatalf("success=%d", success)
	}
}
