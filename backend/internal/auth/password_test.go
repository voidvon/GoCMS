package auth

import "testing"

func TestPasswordHash(t *testing.T) {
	hash, err := HashPassword("123123")
	if err != nil {
		t.Fatal(err)
	}
	if len(hash) < 10 || hash[:10] != "$argon2id$" {
		t.Fatalf("unexpected Argon2id hash: %q", hash)
	}
	if !ComparePassword("123123", hash) {
		t.Fatal("hashed password did not verify")
	}
	if ComparePassword("123124", hash) {
		t.Fatal("wrong password verified")
	}
	if ComparePassword("123123", "bb412a706b8e114d") {
		t.Fatal("legacy hash was accepted")
	}
}

func TestHashPasswordUsesRandomSalt(t *testing.T) {
	first, err := HashPassword("same password")
	if err != nil {
		t.Fatal(err)
	}
	second, err := HashPassword("same password")
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("password hashes reused the salt")
	}
}
