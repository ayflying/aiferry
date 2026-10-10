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

func TestCredentialDisplayIndexesUseSurvivingCredentials(t *testing.T) {
	indexes := credentialDisplayIndexes([]uint64{11, 12})
	if indexes[11] != 1 || indexes[12] != 2 {
		t.Fatalf("unexpected surviving credential ordinals: %v", indexes)
	}
	next := credentialDisplayIndexes([]uint64{11, 12, 13})
	if next[13] != 3 {
		t.Fatalf("new credential must follow surviving credentials: %v", next)
	}
	if len(credentialDisplayIndexes(nil)) != 0 {
		t.Fatal("empty channel must produce an empty index map")
	}
}
