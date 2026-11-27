package main

import (
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

// account 是一把桥接令牌对应的 Google 账号：令牌明文只保存在内存里的摘要形式，
// 真正的登录状态（官方 agy 自己管理的凭据）保存在该账号专属的 HOME 目录。
//
// 设计成「一把令牌 = 一个账号」是为了接入 AiFerry 现有的多账号体系：
// 渠道下每把上游密钥填各自的桥接令牌，AiFerry 按自己的规则挑选密钥并轮换，
// 桥接只负责「用这把令牌对应的账号去调用官方 CLI」，自己不做任何账号选择。
type account struct {
	name string
	// token 是 "Bearer <令牌>" 的 SHA-256，认证时做常数时间比较，不保留明文。
	token [32]byte
	home  string
	// proxy 为空表示该账号没有固定代理，按请求头临时指定的代理或直连。
	proxy string
}

const accountDirName = "accounts"

// loadAccounts 从 stateDir 读取全部账号：
//
//	<stateDir>/bridge.token                 单账号写法（兼容旧部署），视为默认账号
//	<stateDir>/accounts/<名字>/token        多账号写法，每个子目录一个账号
//	<stateDir>/accounts/<名字>/home/        该账号的 HOME，官方 agy 的登录状态存放处
//	<stateDir>/accounts/<名字>/proxy        可选：该账号固定代理地址
//
// 两种写法可以并存；令牌不足 32 字节的目录直接报错，避免弱令牌进入认证环节。
func loadAccounts(stateDir string) ([]account, error) {
	var accounts []account
	if single := strings.TrimSpace(readFile(filepath.Join(stateDir, "bridge.token"))); single != "" {
		item, err := newAccount("default", single, filepath.Join(stateDir, "account-home"), "")
		if err != nil {
			return nil, err
		}
		accounts = append(accounts, item)
	}
	root := filepath.Join(stateDir, accountDirName)
	entries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return accounts, nil
		}
		return nil, err
	}
	for _, entry := range entries {
		if !entry.IsDir() || !validAccountName(entry.Name()) {
			continue
		}
		dir := filepath.Join(root, entry.Name())
		token := strings.TrimSpace(readFile(filepath.Join(dir, "token")))
		if token == "" {
			continue
		}
		home := filepath.Join(dir, "home")
		if err := os.MkdirAll(home, 0700); err != nil {
			return nil, err
		}
		item, err := newAccount(entry.Name(), token, home, strings.TrimSpace(readFile(filepath.Join(dir, "proxy"))))
		if err != nil {
			return nil, err
		}
		accounts = append(accounts, item)
	}
	if len(accounts) == 0 {
		return nil, errors.New("no bridge account found: create accounts/<name>/token with a strong token")
	}
	return accounts, nil
}

// newAccount 校验令牌强度并构建账号；不足 32 字节的令牌一律拒绝。
func newAccount(name, token, home, proxy string) (account, error) {
	if len(token) < 32 {
		return account{}, errors.New("bridge token for account " + name + " must be at least 32 bytes")
	}
	return account{name: name, token: sha256.Sum256([]byte("Bearer " + token)), home: home, proxy: proxy}, nil
}

// validAccountName 限制目录名只含 ASCII 字母、数字、- 和 _，避免路径穿越或奇怪字符进入日志。
func validAccountName(name string) bool {
	if name == "" || len(name) > 64 {
		return false
	}
	for _, r := range name {
		switch {
		case r < 128 && unicode.IsLetter(r), r < 128 && unicode.IsDigit(r), r == '-', r == '_':
		default:
			return false
		}
	}
	return true
}

// resolve 按 Authorization 头找出令牌匹配的账号；没有匹配返回 false。
// 遍历全部账号逐一比较，不因提前命中而泄露「哪一位不对」的时序信息。
func resolve(accounts []account, authorization string) (account, bool) {
	actual := sha256.Sum256([]byte(authorization))
	for _, item := range accounts {
		if subtle.ConstantTimeCompare(item.token[:], actual[:]) == 1 {
			return item, true
		}
	}
	return account{}, false
}

func readFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}
