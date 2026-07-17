package channelgroup

import (
	"context"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/errors/gerror"

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

type Service struct{}

func New() *Service { return &Service{} }

func (s *Service) List(ctx context.Context) ([]View, error) {
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

func (s *Service) Create(ctx context.Context, input adminapi.ChannelGroupInput) (uint64, error) {
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

func (s *Service) Update(ctx context.Context, id uint64, input adminapi.ChannelGroupInput) error {
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
	return dao.ChannelGroups.Transaction(ctx, func(txCtx context.Context, _ gdb.TX) error {
		if _, err := dao.ChannelGroups.Ctx(txCtx).Where(dao.ChannelGroups.Columns().Id, id).Data(do.ChannelGroups{
			Name: strings.TrimSpace(input.Name), Description: strings.TrimSpace(input.Description), Status: normalizeStatus(input.Status),
		}).Update(); err != nil {
			return gerror.Wrap(err, "update channel group")
		}
		return s.replaceMembers(txCtx, id, input.ChannelIDs)
	})
}

func (s *Service) Delete(ctx context.Context, id uint64) error {
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

func (s *Service) ChannelIDs(ctx context.Context, channelID uint64) ([]uint64, error) {
	ids := make([]uint64, 0)
	err := dao.ChannelGroupMembers.Ctx(ctx).Fields(dao.ChannelGroupMembers.Columns().ChannelGroupId).Where(dao.ChannelGroupMembers.Columns().ChannelId, channelID).Scan(&ids)
	return ids, gerror.Wrap(err, "list channel group memberships")
}

func (s *Service) SetChannelIDs(ctx context.Context, channelID uint64, groupIDs []uint64) error {
	if _, err := dao.ChannelGroupMembers.Ctx(ctx).Where(dao.ChannelGroupMembers.Columns().ChannelId, channelID).Delete(); err != nil {
		return gerror.Wrap(err, "remove channel group memberships")
	}
	for _, groupID := range uniqueIDs(groupIDs) {
		if _, err := dao.ChannelGroupMembers.Ctx(ctx).Data(do.ChannelGroupMembers{ChannelGroupId: groupID, ChannelId: channelID}).Insert(); err != nil {
			return gerror.Wrap(err, "add channel group membership")
		}
	}
	return nil
}

func (s *Service) replaceMembers(ctx context.Context, groupID uint64, channelIDs []uint64) error {
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

func (s *Service) view(ctx context.Context, row entity.ChannelGroups) (View, error) {
	ids, err := s.ChannelIDs(ctx, row.Id)
	if err != nil {
		return View{}, err
	}
	view := View{Id: row.Id, Name: row.Name, Code: row.Code, Description: row.Description, Status: row.Status, ChannelIDs: ids}
	view.CreatedAt = row.CreatedAt
	view.UpdatedAt = row.UpdatedAt
	return view, nil
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
