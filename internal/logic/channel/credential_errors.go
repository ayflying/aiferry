package channel

import (
	"errors"

	"github.com/go-sql-driver/mysql"
	"github.com/gogf/gf/v2/errors/gerror"
)

// credentialInsertError 不将带密文和哈希的数据库 SQL 返回给管理端。
func credentialInsertError(err error) error {
	var mysqlErr *mysql.MySQLError
	if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
		return gerror.New("该渠道已添加相同密钥")
	}
	return gerror.New("保存上游密钥失败，请稍后重试")
}
