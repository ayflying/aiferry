package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"io"

	"github.com/gogf/gf/v2/errors/gerror"
)

type Service struct {
	aead cipher.AEAD
}

func New(masterKey []byte) (*Service, error) {
	block, err := aes.NewCipher(masterKey)
	if err != nil {
		return nil, gerror.Wrap(err, "create AES cipher")
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, gerror.Wrap(err, "create AES-GCM cipher")
	}
	return &Service{aead: aead}, nil
}

func (s *Service) Encrypt(plainText string) (string, error) {
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", gerror.Wrap(err, "generate encryption nonce")
	}
	sealed := s.aead.Seal(nonce, nonce, []byte(plainText), nil)
	return base64.RawStdEncoding.EncodeToString(sealed), nil
}

func (s *Service) Decrypt(value string) (string, error) {
	sealed, err := base64.RawStdEncoding.DecodeString(value)
	if err != nil {
		return "", gerror.Wrap(err, "decode encrypted secret")
	}
	nonceSize := s.aead.NonceSize()
	if len(sealed) < nonceSize {
		return "", gerror.New("encrypted secret is truncated")
	}
	plainText, err := s.aead.Open(nil, sealed[:nonceSize], sealed[nonceSize:], nil)
	if err != nil {
		return "", gerror.Wrap(err, "decrypt secret")
	}
	return string(plainText), nil
}

func GenerateAPIKey() (plainText, prefix, hash string, err error) {
	random := make([]byte, 32)
	if _, err = io.ReadFull(rand.Reader, random); err != nil {
		return "", "", "", gerror.Wrap(err, "generate API key")
	}
	plainText = "af_" + base64.RawURLEncoding.EncodeToString(random)
	prefix = plainText[:12]
	hash = HashAPIKey(plainText)
	return plainText, prefix, hash, nil
}

func HashAPIKey(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
