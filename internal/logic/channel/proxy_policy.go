package channel

import (
	"context"
	"net/http"

	"github.com/yunloli/aiferry/internal/model/entity"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/os/gtime"
	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/model/do"
)

// HTTPClientForChannel 供管理请求使用，同样在发送前刷新出口策略，避免长时间
// 运行的定时任务或登录会话持有配置代理之前的渠道快照。
func (s *sChannel) HTTPClientForChannel(ctx context.Context, channel entity.Channels) (*http.Client, error) {
	cipher := channel.ProxyUrlCipher
	if channel.Id > 0 {
		current, err := s.CurrentProxyCipher(ctx, channel.Id)
		if err != nil {
			return nil, err
		}
		cipher = current
	}
	return s.HTTPClientForProxy(cipher)
}

// CurrentProxyCipher 在真正发请求前读取当前出口策略，不信任路由缓存或异步任务
// 中的旧快照。数据库不可用、渠道已删除都必须失败关闭，不能被解释成无代理。
func (s *sChannel) CurrentProxyCipher(ctx context.Context, channelID uint64) (string, error) {
	var row struct {
		ID          uint64      `orm:"id"`
		ProxyCipher string      `orm:"proxy_url_cipher"`
		DeletedAt   *gtime.Time `orm:"deleted_at"`
	}
	cols := dao.Channels.Columns()
	if err := dao.Channels.Ctx(ctx).Fields(cols.Id, cols.ProxyUrlCipher, cols.DeletedAt).Where(do.Channels{Id: channelID}).Scan(&row); err != nil {
		return "", gerror.New("读取渠道代理策略失败；为保护账号已阻止上游请求")
	}
	// 部分节点使用 SQLite，投影到非 entity 结构时不依赖 ORM 自动软删过滤；
	// 显式检查已读出的删除标记，避免旧任务继续使用被删除渠道。
	if row.ID == 0 || row.DeletedAt != nil {
		return "", gerror.New("渠道不存在；为保护账号已阻止上游请求")
	}
	return row.ProxyCipher, nil
}
