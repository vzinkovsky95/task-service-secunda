package user

import (
	"strings"
	"testing"
)

func TestNormalizeEmail(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"  User@Example.COM ", "user@example.com", true},
		{"a@b.co", "a@b.co", true},
		{"", "", false},
		{"no-at-sign", "", false},
		{"Name <a@b.co>", "", false},
		{"a@b.co, c@d.co", "", false},
		{strings.Repeat("a", 250) + "@b.co", "", false},
	}
	for _, tc := range cases {
		got, err := normalizeEmail(tc.in)
		if (err == nil) != tc.ok || got != tc.want {
			t.Errorf("normalizeEmail(%q) = %q, %v", tc.in, got, err)
		}
	}
}

func TestValidatePassword(t *testing.T) {
	cases := map[string]bool{
		"1234567":               false,
		"12345678":              true,
		strings.Repeat("a", 72): true,
		strings.Repeat("a", 73): false,
		strings.Repeat("я", 37): false,
	}
	for password, ok := range cases {
		if err := validatePassword(password); (err == nil) != ok {
			t.Errorf("validatePassword(%d bytes) err = %v", len(password), err)
		}
	}
}

func TestNormalizeName(t *testing.T) {
	if got, err := normalizeName("  Вадим  "); err != nil || got != "Вадим" {
		t.Errorf("got %q, %v", got, err)
	}
	if _, err := normalizeName("   "); err == nil {
		t.Error("blank name accepted")
	}
	if _, err := normalizeName(strings.Repeat("я", 256)); err == nil {
		t.Error("long name accepted")
	}
	if _, err := normalizeName(strings.Repeat("я", 255)); err != nil {
		t.Errorf("255 runes rejected: %v", err)
	}
}
