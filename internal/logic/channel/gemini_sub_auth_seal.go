package channel

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"

	"github.com/gogf/gf/v2/errors/gerror"
)

// AiFerry 通用 Secrets 当前是明文透传。OAuth 多级票据使用运营方专用客户端密钥
// 派生独立密钥；不把 OAuth 客户端密钥写到缓存或数据库。
func (cfg geminiOAuthConfig) sealCipher() (cipher.AEAD, error) {
	key, err := hkdf.Key(sha256.New, []byte(cfg.clientSecret), []byte(cfg.clientID), "aiferry/gemini-subscription/oauth/v1", 32)
	if err != nil {
		return nil, gerror.New("初始化 Gemini OAuth 密封失败")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, gerror.New("初始化 Gemini OAuth 密封失败")
	}
	return cipher.NewGCMWithRandomNonce(block)
}

func (cfg geminiOAuthConfig) seal(aad string, data any) (string, error) {
	body, err := json.Marshal(data)
	if err != nil {
		return "", gerror.New("编码 Gemini OAuth 数据失败")
	}
	gcm, err := cfg.sealCipher()
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(gcm.Seal(nil, nil, body, []byte(aad))), nil
}

func (cfg geminiOAuthConfig) open(aad, sealed string, dest any) error {
	gcm, err := cfg.sealCipher()
	if err != nil {
		return err
	}
	data, err := base64.RawURLEncoding.DecodeString(sealed)
	if err != nil {
		return gerror.New("Gemini OAuth 数据无效，请重新授权")
	}
	body, err := gcm.Open(nil, nil, data, []byte(aad))
	if err != nil || json.Unmarshal(body, dest) != nil {
		return gerror.New("Gemini OAuth 数据无效或客户端配置已变更，请重新授权")
	}
	return nil
}

func geminiPKCE() (verifier, challenge string, err error) {
	var entropy [32]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		return "", "", gerror.New("生成 Gemini OAuth PKCE 失败")
	}
	verifier = base64.RawURLEncoding.EncodeToString(entropy[:])
	hash := sha256.Sum256([]byte(verifier))
	return verifier, base64.RawURLEncoding.EncodeToString(hash[:]), nil
}
