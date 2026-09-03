package channelgroup

import (
	"context"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/redis/go-redis/v9"

	adminapi "github.com/yunloli/aiferry/api/admin"
	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/model/do"
	"github.com/yunloli/aiferry/internal/model/entity"
)

var codePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{1,63}$`)

type View struct {
	Id          uint64    `json:"id"`
	Name        string    `json:"name"`
	Code        string    `json:"code"`
	Description string    `json:"description"`
	Status      int       `json:"status"`
	ChannelIDs  []uint64  `json:"channelIds"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type sChannelGroup struct {
	redis *redis.Client
}

func New(redisClient *redis.Client) *sChannelGroup { return &sChannelGroup{redis: redisClient} }

// invalidateRoutes 递增路由缓存版本号。路由候选缓存中包含渠道分组 ID 与
// 分组状态（channelGroupIDs），分组创建/更新/删除/成员变更后必须失效，
// 否则转发侧最长 30 分钟内仍使用旧分组归属路由请求。
func (s *sChannelGroup) invalidateRoutes(ctx context.Context) {
	if s.redis == nil {
		return
	}
	_ = s.redis.Incr(ctx, "aiferry:routes:version").Err()
}

func (s *sChannelGroup) List(ctx context.Context) ([]View, error) {
	var rows []entity.ChannelGroups
	if err := dao.ChannelGroups.Ctx(ctx).OrderAsc(dao.ChannelGroups.Columns().Name).Scan(&rows); err != nil {
		return nil, gerror.Wrap(err, "list channel groups")
	}
	result := make([]View, 0, len(rows))
	for _, row := range rows {
		view, err := s.view(ctx, row)
		if err != nil {
			return nil, err
		}
		result = append(result, view)
	}
	return result, nil
}

func (s *sChannelGroup) Create(ctx context.Context, input adminapi.ChannelGroupInput) (uint64, error) {
	code := strings.TrimSpace(input.Code)
	if !codePattern.MatchString(code) {
		return 0, gerror.New("channel group code must start with a lowercase letter and contain only lowercase letters, numbers, underscores, or hyphens")
	}
	var id uint64
	err := dao.ChannelGroups.Transaction(ctx, func(txCtx context.Context, _ gdb.TX) error {
		created, err := dao.ChannelGroups.Ctx(txCtx).Data(do.ChannelGroups{
			Name: strings.TrimSpace(input.Name), Code: code, Description: strings.TrimSpace(input.Description), Status: normalizeStatus(input.Status),
		}).InsertAndGetId()
		if err != nil {
			return gerror.Wrap(err, "create channel group")
		}
		id = uint64(created)
		return s.replaceMembers(txCtx, id, input.ChannelIDs)
	})
	return id, err
}

func (s *sChannelGroup) Update(ctx context.Context, id uint64, input adminapi.ChannelGroupInput) error {
	var current entity.ChannelGroups
	if err := dao.ChannelGroups.Ctx(ctx).Where(dao.ChannelGroups.Columns().Id, id).Scan(&current); err != nil {
		return gerror.Wrap(err, "find channel group")
	}
	if current.Id == 0 {
		return gerror.New("channel group not found")
	}
	if strings.TrimSpace(input.Code) != current.Code {
		return gerror.New("channel group code cannot be changed")
	}
	err := dao.ChannelGroups.Transaction(ctx, func(txCtx context.Context, _ gdb.TX) error {
		if _, err := dao.ChannelGroups.Ctx(txCtx).Where(dao.ChannelGroups.Columns().Id, id).Data(do.ChannelGroups{
			Name: strings.TrimSpace(input.Name), Description: strings.TrimSpace(input.Description), Status: normalizeStatus(input.Status),
		}).Update(); err != nil {
			return gerror.Wrap(err, "update channel group")
		}
		return s.replaceMembers(txCtx, id, input.ChannelIDs)
	})
	if err == nil {
		s.invalidateRoutes(ctx)
	}
	return err
}

func (s *sChannelGroup) Delete(ctx context.Context, id uint64) error {
	return dao.ChannelGroups.Transaction(ctx, func(txCtx context.Context, _ gdb.TX) error {
		if _, err := dao.ChannelGroupMembers.Ctx(txCtx).Where(dao.ChannelGroupMembers.Columns().ChannelGroupId, id).Delete(); err != nil {
			return gerror.Wrap(err, "remove channel group members")
		}
		if _, err := dao.ApiKeyChannelGroups.Ctx(txCtx).Where(dao.ApiKeyChannelGroups.Columns().ChannelGroupId, id).Delete(); err != nil {
			return gerror.Wrap(err, "remove key group policies")
		}
		result, err := dao.ChannelGroups.Ctx(txCtx).Where(dao.ChannelGroups.Columns().Id, id).Delete()
		if err != nil {
			return gerror.Wrap(err, "delete channel group")
		}
		if affected, _ := result.RowsAffected(); affected == 0 {
			return gerror.New("channel group not found")
		}
		return nil
	})
}

func (s *sChannelGroup) ChannelIDs(ctx context.Context, channelID uint64) ([]uint64, error) {
	rows := make([]entity.ChannelGroupMembers, 0)
	err := dao.ChannelGroupMembers.Ctx(ctx).
		Fields(dao.ChannelGroupMembers.Columns().ChannelGroupId).
		Where(dao.ChannelGroupMembers.Columns().ChannelId, channelID).
		Scan(&rows)
	if err != nil {
		return nil, gerror.Wrap(err, "list channel group memberships")
	}
	ids := make([]uint64, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ChannelGroupId)
	}
	return ids, nil
}

func (s *sChannelGroup) SetChannelIDs(ctx context.Context, channelID uint64, groupIDs []uint64) error {
	if _, err := dao.ChannelGroupMembers.Ctx(ctx).Where(dao.ChannelGroupMembers.Columns().ChannelId, channelID).Delete(); err != nil {
		return gerror.Wrap(err, "remove channel group memberships")
	}
	for _, groupID := range uniqueIDs(groupIDs) {
		if _, err := dao.ChannelGroupMembers.Ctx(ctx).Data(do.ChannelGroupMembers{ChannelGroupId: groupID, ChannelId: channelID}).Insert(); err != nil {
			return gerror.Wrap(err, "add channel group membership")
		}
	}
	s.invalidateRoutes(ctx)
	return nil
}

func (s *sChannelGroup) replaceMembers(ctx context.Context, groupID uint64, channelIDs []uint64) error {
	if _, err := dao.ChannelGroupMembers.Ctx(ctx).Where(dao.ChannelGroupMembers.Columns().ChannelGroupId, groupID).Delete(); err != nil {
		return gerror.Wrap(err, "remove channel group members")
	}
	for _, channelID := range uniqueIDs(channelIDs) {
		if _, err := dao.ChannelGroupMembers.Ctx(ctx).Data(do.ChannelGroupMembers{ChannelGroupId: groupID, ChannelId: channelID}).Insert(); err != nil {
			return gerror.Wrap(err, "add channel group member")
		}
	}
	return nil
}

func (s *sChannelGroup) view(ctx context.Context, row entity.ChannelGroups) (View, error) {
	ids, err := s.memberChannelIDs(ctx, row.Id)
	if err != nil {
		return View{}, err
	}
	view := View{Id: row.Id, Name: row.Name, Code: row.Code, Description: row.Description, Status: row.Status, ChannelIDs: ids}
	view.CreatedAt = row.CreatedAt
	view.UpdatedAt = row.UpdatedAt
	return view, nil
}

func (s *sChannelGroup) memberChannelIDs(ctx context.Context, groupID uint64) ([]uint64, error) {
	rows := make([]entity.ChannelGroupMembers, 0)
	err := dao.ChannelGroupMembers.Ctx(ctx).
		Fields(dao.ChannelGroupMembers.Columns().ChannelId).
		Where(dao.ChannelGroupMembers.Columns().ChannelGroupId, groupID).
		Scan(&rows)
	if err != nil {
		return nil, gerror.Wrap(err, "list channel group members")
	}
	ids := make([]uint64, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ChannelId)
	}
	return ids, nil
}

func normalizeStatus(value int) int {
	if value == 0 {
		return 0
	}
	return 1
}

func uniqueIDs(values []uint64) []uint64 {
	seen := make(map[uint64]struct{}, len(values))
	result := make([]uint64, 0, len(values))
	for _, value := range values {
		if value > 0 {
			seen[value] = struct{}{}
		}
	}
	for value := range seen {
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}
