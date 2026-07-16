package secret

import (
	"bytes"
	"strings"
	"testing"
)

func TestEncryptDecrypt(t *testing.T) {
	service, err := New(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	encrypted, err := service.Encrypt("sk-sensitive")
	if err != nil {
		t.Fatal(err)
	}
	if encrypted == "sk-sensitive" || strings.Contains(encrypted, "sensitive") {
		t.Fatal("ciphertext exposes plaintext")
	}
	plainText, err := service.Decrypt(encrypted)
	if err != nil {
		t.Fatal(err)
	}
	if plainText != "sk-sensitive" {
		t.Fatalf("unexpected plaintext: %q", plainText)
	}
}

func TestGenerateAPIKey(t *testing.T) {
	plainText, prefix, hash, err := GenerateAPIKey()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(plainText, "af_") || prefix != plainText[:12] {
		t.Fatalf("invalid key shape: %q %q", plainText, prefix)
	}
	if hash != HashAPIKey(plainText) || len(hash) != 64 {
		t.Fatal("invalid API key hash")
	}
}
