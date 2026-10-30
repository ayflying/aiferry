package app

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "github.com/gogf/gf/contrib/drivers/sqlite/v2"
	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/errors/gerror"

	"github.com/yunloli/aiferry/internal/logic/cache"
)

//go:embed schema.sql
var workerSchema string

// openWorker 装配子节点：不连 MySQL、不连 Redis、不执行迁移，
// 只把内置 SQLite 注册为 default 数据库组，让既有 DAO 读路径直接复用。
func (s *sApp) openWorker(ctx context.Context) error {
	dataDir := s.Config.Cluster.DataDir
	if err := os.MkdirAll(dataDir, 0o750); err != nil {
		return gerror.Wrap(err, "create worker data directory")
	}
	path := filepath.Join(dataDir, "worker.db")
	if err := gdb.AddConfigNode("default", gdb.ConfigNode{Type: "sqlite", Link: sqliteLink(path)}); err != nil {
		return gerror.Wrap(err, "configure worker sqlite database")
	}
	instance, err := gdb.Instance()
	if err != nil {
		return gerror.Wrap(err, "open worker sqlite database")
	}
	if err = execSchema(ctx, instance); err != nil {
		return err
	}
	store, err := cache.NewSQLite(ctx, instance)
	if err != nil {
		return err
	}
	s.Redis = store
	return nil
}

// sqliteLink 生成 GoFrame sqlite 连接串。子节点只有这一个本地文件依赖。
func sqliteLink(path string) string {
	return fmt.Sprintf("sqlite::@file(%s)", filepath.ToSlash(path))
}

// execSchema 执行内置建表脚本，全部语句都带 IF NOT EXISTS，可重复执行。
func execSchema(ctx context.Context, instance gdb.DB) error {
	for _, statement := range schemaStatements(workerSchema) {
		if _, err := instance.Exec(ctx, statement); err != nil {
			return gerror.Wrapf(err, "apply worker schema statement: %.80s", statement)
		}
	}
	return nil
}

// schemaStatements 把建表脚本切成单条语句。
//
// 先整行剔除 "--" 注释再按分号切分：注释里出现分号、或建表语句前带注释时，
// 直接按 ";\n" 切分会把注释连同语句一起丢掉，导致缺表却毫无报错。
func schemaStatements(script string) []string {
	lines := make([]string, 0, 128)
	for _, line := range strings.Split(script, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "--") {
			continue
		}
		lines = append(lines, line)
	}
	statements := make([]string, 0, 64)
	for _, part := range strings.Split(strings.Join(lines, "\n"), ";") {
		if statement := strings.TrimSpace(part); statement != "" {
			statements = append(statements, statement)
		}
	}
	return statements
}
