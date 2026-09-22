package channel

import (
	"database/sql"
	"fmt"
	"testing"
)

func TestMaskedCredentialPrefixDoesNotExposeTheFullSecret(t *testing.T) {
	secret := "sk-abcdefghijklmnopqrstuvwxyz0123456789"
	masked := maskedCredentialPrefix(secret)
	if masked == secret || masked != "sk-abcde..." {
		t.Fatalf("unexpected masked credential prefix: %q", masked)
	}
	if maskedCredentialPrefix("short") != "已配置" {
		t.Fatal("short credentials must not be returned to the client")
	}
}

func TestUpstreamKeyHashIsStableAndSecretSpecific(t *testing.T) {
	first := upstreamKeyHash(" sk-example ")
	if first != upstreamKeyHash("sk-example") {
		t.Fatal("credential hash should ignore surrounding whitespace")
	}
	if first == upstreamKeyHash("sk-other") {
		t.Fatal("different credentials must not share a hash")
	}
}

func TestMissingCredentialBindingErrorOnlyMatchesNoRows(t *testing.T) {
	if !isMissingCredentialBindingError(sql.ErrNoRows) {
		t.Fatal("sql.ErrNoRows should mean first credential binding")
	}
	if !isMissingCredentialBindingError(fmt.Errorf("wrapped: %w", sql.ErrNoRows)) {
		t.Fatal("wrapped sql.ErrNoRows should mean first credential binding")
	}
	if isMissingCredentialBindingError(fmt.Errorf("connection refused")) {
		t.Fatal("database failures must not be treated as missing binding")
	}
}

func TestCredentialDisplayIndexesKeepDeletedSlots(t *testing.T) {
	// Unscoped 全量（含已软删）按 id 升序：10(#1) 已删、11(#2)、12(#3)。
	// 列表只展示存活的 11/12，但它们必须保持原编号，与历史「渠道 #N」一致。
	indexes := credentialDisplayIndexes([]uint64{10, 11, 12})
	if indexes[11] != 2 || indexes[12] != 3 {
		t.Fatalf("surviving credentials must keep their original ordinal, got %v", indexes)
	}
	if indexes[10] != 1 {
		t.Fatalf("deleted credential must keep occupying its slot, got %v", indexes)
	}
	// 删除 #1 后再新增：新密钥 id 最大，拿到的是历史总数+1，不是 1。
	next := credentialDisplayIndexes([]uint64{10, 11, 12, 13})
	if next[13] != 4 {
		t.Fatalf("credential added after deletion must continue the sequence, got %v", next)
	}
	if len(credentialDisplayIndexes(nil)) != 0 {
		t.Fatal("empty channel must produce an empty index map")
	}
}
