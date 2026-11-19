package secret

import (
	"strings"
	"testing"
)

// 敏感字段不再加密：Encrypt / Decrypt 必须原样往返，保证数据库中看到的就是明文。
func TestEncryptDecryptPassesThrough(t *testing.T) {
	service := New()
	stored, err := service.Encrypt("sk-sensitive")
	if err != nil {
		t.Fatal(err)
	}
	if stored != "sk-sensitive" {
		t.Fatalf("写库前不应再变换内容，实际 %q", stored)
	}
	plainText, err := service.Decrypt(stored)
	if err != nil {
		t.Fatal(err)
	}
	if plainText != "sk-sensitive" {
		t.Fatalf("读取结果应与写入一致，实际 %q", plainText)
	}
}

// 空值要能原样往返：渠道允许清空 api_key_cipher 表示未配置。
func TestEncryptDecryptHandlesEmptyValue(t *testing.T) {
	service := New()
	stored, err := service.Encrypt("")
	if err != nil || stored != "" {
		t.Fatalf("空值加密结果应为空，实际 %q err=%v", stored, err)
	}
	plainText, err := service.Decrypt("")
	if err != nil || plainText != "" {
		t.Fatalf("空值解密结果应为空，实际 %q err=%v", plainText, err)
	}
}

func TestGenerateAPIKey(t *testing.T) {
	plainText, prefix, hash, err := GenerateAPIKey()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(plainText, APIKeyPrefix) || prefix != plainText[:12] {
		t.Fatalf("invalid key shape: %q %q", plainText, prefix)
	}
	if hash != HashAPIKey(plainText) || len(hash) != 64 {
		t.Fatal("invalid API key hash")
	}
}

func TestHasAPIKeyPrefix(t *testing.T) {
	if !HasAPIKeyPrefix("sk-current") || !HasAPIKeyPrefix("af_legacy") {
		t.Fatal("current and legacy API key prefixes should be accepted")
	}
	if HasAPIKeyPrefix("pk-invalid") {
		t.Fatal("unexpected API key prefix accepted")
	}
}
