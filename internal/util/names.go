package util

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
)

var nonAlphanumericHyphen = regexp.MustCompile(`[^a-z0-9-]`)

// SanitizeResourceName produces an AWS-safe name: lowercase, alphanumeric + hyphens,
// with a short hash suffix for uniqueness, max 40 chars total.
func SanitizeResourceName(raw string) string {
	name := strings.ToLower(raw)
	name = strings.ReplaceAll(name, "_", "-")
	name = nonAlphanumericHyphen.ReplaceAllString(name, "")
	for strings.Contains(name, "--") {
		name = strings.ReplaceAll(name, "--", "-")
	}
	name = strings.Trim(name, "-")

	h := sha256.Sum256([]byte(raw))
	hash := hex.EncodeToString(h[:4]) // 8 hex chars

	maxPrefix := 40 - len(hash) - 1 // 1 for the hyphen separator
	if len(name) > maxPrefix {
		name = name[:maxPrefix]
	}
	name = strings.TrimRight(name, "-")

	return fmt.Sprintf("%s-%s", name, hash)
}

// HashString returns the full SHA-256 hex hash of the input.
func HashString(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}
