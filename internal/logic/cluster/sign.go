package cluster

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"

	"github.com/yunloli/aiferry/internal/logic/cache"
)

// 节点间签名使用的请求头。主节点与子节点共用一套约定，全部走内网明文 HTTP，
// 签名只负责身份与完整性，不承担加密职责。
const (
	HeaderNode      = "X-Aiferry-Node"
	HeaderTimestamp = "X-Aiferry-Ts"
	HeaderNonce     = "X-Aiferry-Nonce"
	HeaderSignature = "X-Aiferry-Signature"

	// signWindow 是允许的时钟偏差。超出窗口的请求一律拒绝，避免重放。
	signWindow = 5 * time.Minute
	// nonceTTL 是已用 nonce 的保留时长，必须不短于两倍 signWindow。
	nonceTTL = 15 * time.Minute
	nonceKey = "aiferry:cluster:nonce:"
)

// Signer 负责节点间 HMAC-SHA256 签名与验签。
type Signer struct {
	secret string
	store  cache.Cache
	now    func() time.Time
}

// NewSigner 构造签名器。store 用于记录已用 nonce，可为 nil（此时不做重放校验，
// 仅用于子节点侧构造请求）。
func NewSigner(secret string, store cache.Cache) *Signer {
	return &Signer{secret: secret, store: store, now: time.Now}
}

// payload 把请求要素拼成待签名串。body 用摘要而不是原文，避免超长报文参与拼接。
func payload(node, timestamp, nonce, method, path string, body []byte) string {
	digest := sha256.Sum256(body)
	return strings.Join([]string{
		node,
		timestamp,
		nonce,
		strings.ToUpper(method),
		path,
		hex.EncodeToString(digest[:]),
	}, "\n")
}

func (s *Signer) signature(text string) string {
	mac := hmac.New(sha256.New, []byte(s.secret))
	mac.Write([]byte(text))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// Headers 为一次出站请求生成签名头。
func (s *Signer) Headers(node, method, path string, body []byte) (http.Header, error) {
	nonce, err := newNonce()
	if err != nil {
		return nil, err
	}
	timestamp := strconv.FormatInt(s.now().Unix(), 10)
	headers := make(http.Header, 4)
	headers.Set(HeaderNode, node)
	headers.Set(HeaderTimestamp, timestamp)
	headers.Set(HeaderNonce, nonce)
	headers.Set(HeaderSignature, s.signature(payload(node, timestamp, nonce, method, path, body)))
	return headers, nil
}

// Verify 校验入站请求的签名，返回通过校验的节点名。
// body 是已经读取完毕的请求体（调用方负责读一次并回填）。
func (s *Signer) Verify(ctx context.Context, r *http.Request, body []byte) (string, error) {
	node := strings.TrimSpace(r.Header.Get(HeaderNode))
	timestamp := strings.TrimSpace(r.Header.Get(HeaderTimestamp))
	nonce := strings.TrimSpace(r.Header.Get(HeaderNonce))
	signature := strings.TrimSpace(r.Header.Get(HeaderSignature))
	if node == "" || timestamp == "" || nonce == "" || signature == "" {
		return "", gerror.New("cluster: missing signature headers")
	}
	seconds, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return "", gerror.New("cluster: invalid signature timestamp")
	}
	skew := s.now().Sub(time.Unix(seconds, 0))
	if skew > signWindow || skew < -signWindow {
		return "", gerror.Newf("cluster: signature timestamp out of window (%s)", skew.Round(time.Second))
	}
	expected := s.signature(payload(node, timestamp, nonce,
		r.Method, r.URL.Path, body))
	if !hmac.Equal([]byte(expected), []byte(signature)) {
		return "", gerror.New("cluster: signature mismatch")
	}
	if s.store != nil {
		// SetNX 保证同一 nonce 只能成功一次，从而让重放请求在时间窗内也失效。
		created, err := s.store.SetNX(ctx, nonceKey+nonce, node, nonceTTL).Result()
		if err != nil {
			return "", gerror.Wrap(err, "cluster: record request nonce")
		}
		if !created {
			return "", gerror.New("cluster: duplicated request nonce")
		}
	}
	return node, nil
}

func newNonce() (string, error) {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return "", gerror.Wrap(err, "cluster: generate nonce")
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}
