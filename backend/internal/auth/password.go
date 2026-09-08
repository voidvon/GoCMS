package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	argonVersion    = "v=19"
	argonMemory     = 64 * 1024
	argonTime       = 3
	argonThreads    = 2
	argonSaltLength = 16
	argonKeyLength  = 32
)

// HashPassword returns a PHC-formatted Argon2id password hash.
func HashPassword(password string) (string, error) {
	if password == "" {
		return "", errors.New("password cannot be empty")
	}

	salt := make([]byte, argonSaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate password salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLength)

	return fmt.Sprintf(
		"$argon2id$%s$m=%d,t=%d,p=%d$%s$%s",
		argonVersion,
		argonMemory,
		argonTime,
		argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

// ComparePassword verifies a password against a PHC-formatted Argon2id hash.
func ComparePassword(password, encoded string) bool {
	memory, timeCost, threads, salt, expected, ok := parseHash(encoded)
	if !ok {
		return false
	}
	derived := argon2.IDKey([]byte(password), salt, timeCost, memory, threads, uint32(len(expected)))
	return subtle.ConstantTimeCompare(derived, expected) == 1
}

func parseHash(encoded string) (uint32, uint32, uint8, []byte, []byte, bool) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" || parts[2] != argonVersion {
		return 0, 0, 0, nil, nil, false
	}

	var memory, timeCost uint64
	var threads uint64
	for _, parameter := range strings.Split(parts[3], ",") {
		keyValue := strings.SplitN(parameter, "=", 2)
		if len(keyValue) != 2 {
			return 0, 0, 0, nil, nil, false
		}
		value, err := strconv.ParseUint(keyValue[1], 10, 32)
		if err != nil {
			return 0, 0, 0, nil, nil, false
		}
		switch keyValue[0] {
		case "m":
			memory = value
		case "t":
			timeCost = value
		case "p":
			threads = value
		default:
			return 0, 0, 0, nil, nil, false
		}
	}
	if memory < 8*1024 || memory > 256*1024 || timeCost < 1 || timeCost > 10 || threads < 1 || threads > 8 {
		return 0, 0, 0, nil, nil, false
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) < 16 || len(salt) > 64 {
		return 0, 0, 0, nil, nil, false
	}
	expected, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(expected) < 16 || len(expected) > 64 {
		return 0, 0, 0, nil, nil, false
	}

	return uint32(memory), uint32(timeCost), uint8(threads), salt, expected, true
}
