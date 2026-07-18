package channel

import "testing"

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
