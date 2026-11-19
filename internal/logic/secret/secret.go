package secret

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"io"
	"strings"

	"github.com/gogf/gf/v2/errors/gerror"
)

const (
	APIKeyPrefix       = "sk-"
	legacyAPIKeyPrefix = "af_"
)

// Service 负责数据库里敏感字段的读写。
//
// 2026-11-19 起不再加密。原来的 AIFERRY_MASTER_KEY 方案有两个无法回避的问题：
// 主密钥一旦与加密时使用的值不一致，所有渠道密钥都会解不开（报
// cipher: message authentication failed），而换一种部署方式就必然要重新配一次
// 主密钥，配错一次就是全部渠道失效。运维成本明显高于它带来的收益。
//
// 现在渠道密钥、管理密钥、Codex 凭据、代理地址、SMTP 密码一律明文存库，
// Encrypt / Decrypt 保留下来只是为了让这 8 类字段的读写路径不必改动：
// 字段名仍带 cipher 后缀，改名要迁移表结构，收益不大。
//
// 安全边界随之改变：能读到数据库或其备份的人就能直接看到这些密钥。
// 部署时应把 MySQL 限制在内网、不要外露端口，并用独立的低权限账号连接。
type Service struct{}

// New 构造读写服务。原来需要主密钥，现在不再需要任何参数。
func New() *Service {
	return &Service{}
}

// Encrypt 原样返回：写库时不做任何变换。
func (s *Service) Encrypt(plainText string) (string, error) {
	return plainText, nil
}

// Decrypt 原样返回：库里存的已经是明文。
func (s *Service) Decrypt(value string) (string, error) {
	return value, nil
}

func GenerateAPIKey() (plainText, prefix, hash string, err error) {
	random := make([]byte, 32)
	if _, err = io.ReadFull(rand.Reader, random); err != nil {
		return "", "", "", gerror.Wrap(err, "generate API key")
	}
	plainText = APIKeyPrefix + base64.RawURLEncoding.EncodeToString(random)
	prefix = plainText[:12]
	hash = HashAPIKey(plainText)
	return plainText, prefix, hash, nil
}

func HasAPIKeyPrefix(value string) bool {
	return strings.HasPrefix(value, APIKeyPrefix) || strings.HasPrefix(value, legacyAPIKeyPrefix)
}

func HashAPIKey(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
