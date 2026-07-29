package redemption

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/hex"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/shopspring/decimal"

	adminapi "github.com/yunloli/aiferry/api/admin"
	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/logic/user"
	"github.com/yunloli/aiferry/internal/model/do"
)

const (
	statusActive  = "active"
	statusUsed    = "used"
	statusExpired = "expired"
)

type sRedemption struct {
	users *user.Service
}

type View struct {
	Id             uint64     `json:"id"`
	Name           string     `json:"name"`
	Code           string     `json:"code"`
	Amount         float64    `json:"amount"`
	Status         string     `json:"status"`
	ExpiresAt      *time.Time `json:"expiresAt"`
	RedeemedByName string     `json:"redeemedByName"`
	RedeemedAt     *time.Time `json:"redeemedAt"`
	CreatedAt      time.Time  `json:"createdAt"`
}

type CreatedCode struct {
	Id        uint64     `json:"id"`
	Code      string     `json:"code"`
	Amount    float64    `json:"amount"`
	ExpiresAt *time.Time `json:"expiresAt"`
}

type RedeemResult struct {
	Code   string  `json:"code"`
	Amount float64 `json:"amount"`
}

type ListFilter struct {
	Keyword string
	Status  string
}

type codeRow struct {
	Id               uint64     `orm:"id"`
	Name             string     `orm:"name"`
	Code             string     `orm:"code"`
	Amount           string     `orm:"amount"`
	ExpiresAt        *time.Time `orm:"expires_at"`
	RedeemedByUserId *uint64    `orm:"redeemed_by_user_id"`
	RedeemedAt       *time.Time `orm:"redeemed_at"`
	CreatedAt        time.Time  `orm:"created_at"`
}

func New(userSvc *user.Service) *sRedemption {
	return &sRedemption{users: userSvc}
}

func (s *sRedemption) Create(ctx context.Context, operatorID uint64, input adminapi.RedemptionCodeCreateInput) ([]CreatedCode, error) {
	name, amount, expiresAt, err := normalizeCreateInput(input)
	if err != nil {
		return nil, err
	}
	created := make([]CreatedCode, 0, input.Quantity)
	err = dao.RedemptionCodes.Transaction(ctx, func(txCtx context.Context, _ gdb.TX) error {
		for range input.Quantity {
			code, codeErr := generateCode()
			if codeErr != nil {
				return codeErr
			}
			id, insertErr := dao.RedemptionCodes.Ctx(txCtx).Data(do.RedemptionCodes{
				Name: name, Code: code, Amount: amount.StringFixed(8), ExpiresAt: expiresAt, CreatedByUserId: operatorID,
			}).InsertAndGetId()
			if insertErr != nil {
				return gerror.Wrap(insertErr, "create redemption code")
			}
			created = append(created, CreatedCode{Id: uint64(id), Code: code, Amount: decimalFloat(amount), ExpiresAt: expiresAt})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}

func (s *sRedemption) List(ctx context.Context, filter ListFilter) ([]View, error) {
	now := time.Now()
	keyword, status, err := normalizeListFilter(filter)
	if err != nil {
		return nil, err
	}
	rows := make([]codeRow, 0)
	if err = dao.RedemptionCodes.Ctx(ctx).OrderDesc(dao.RedemptionCodes.Columns().CreatedAt).Scan(&rows); err != nil {
		return nil, gerror.Wrap(err, "list redemption codes")
	}
	filtered := make([]codeRow, 0, len(rows))
	for _, row := range rows {
		if redemptionCodeMatches(row, keyword, status, now) {
			filtered = append(filtered, row)
		}
	}
	return s.views(ctx, filtered, now)
}

func (s *sRedemption) DeleteInvalid(ctx context.Context) (int, error) {
	now := time.Now()
	rows := make([]codeRow, 0)
	if err := dao.RedemptionCodes.Ctx(ctx).Scan(&rows); err != nil {
		return 0, gerror.Wrap(err, "load redemption codes for cleanup")
	}
	ids := make([]uint64, 0)
	for _, row := range rows {
		if row.RedeemedAt != nil || (row.ExpiresAt != nil && !row.ExpiresAt.After(now)) {
			ids = append(ids, row.Id)
		}
	}
	if len(ids) == 0 {
		return 0, nil
	}
	result, err := dao.RedemptionCodes.Ctx(ctx).
		WhereIn(dao.RedemptionCodes.Columns().Id, ids).
		Delete()
	if err != nil {
		return 0, gerror.Wrap(err, "delete invalid redemption codes")
	}
	deleted, err := result.RowsAffected()
	if err != nil {
		return 0, gerror.Wrap(err, "count deleted redemption codes")
	}
	return int(deleted), nil
}

func (s *sRedemption) views(ctx context.Context, rows []codeRow, now time.Time) ([]View, error) {
	userIDs := make([]uint64, 0, len(rows))
	for _, row := range rows {
		if row.RedeemedByUserId != nil {
			userIDs = append(userIDs, *row.RedeemedByUserId)
		}
	}
	names, err := s.redeemerNames(ctx, userIDs)
	if err != nil {
		return nil, err
	}
	result := make([]View, 0, len(rows))
	for _, row := range rows {
		amount, err := decimal.NewFromString(row.Amount)
		if err != nil {
			return nil, gerror.Wrap(err, "parse redemption code amount")
		}
		view := View{
			Id: row.Id, Name: row.Name, Code: row.Code, Amount: decimalFloat(amount), Status: codeStatus(row, now),
			ExpiresAt: row.ExpiresAt, RedeemedAt: row.RedeemedAt, CreatedAt: row.CreatedAt,
		}
		if row.RedeemedByUserId != nil {
			view.RedeemedByName = names[*row.RedeemedByUserId]
		}
		result = append(result, view)
	}
	return result, nil
}

func (s *sRedemption) redeemerNames(ctx context.Context, userIDs []uint64) (map[uint64]string, error) {
	if len(userIDs) == 0 {
		return map[uint64]string{}, nil
	}
	rows := make([]struct {
		Id   uint64 `orm:"id"`
		Name string `orm:"name"`
	}, 0)
	if err := dao.Users.Ctx(ctx).Fields(dao.Users.Columns().Id, dao.Users.Columns().Name).WhereIn(dao.Users.Columns().Id, userIDs).Scan(&rows); err != nil {
		return nil, gerror.Wrap(err, "list redemption code users")
	}
	result := make(map[uint64]string, len(rows))
	for _, row := range rows {
		result[row.Id] = row.Name
	}
	return result, nil
}

func normalizeCreateInput(input adminapi.RedemptionCodeCreateInput) (string, decimal.Decimal, *time.Time, error) {
	name := strings.TrimSpace(input.Name)
	if utf8.RuneCountInString(name) == 0 || utf8.RuneCountInString(name) > 20 {
		return "", decimal.Zero, nil, gerror.New("兑换码名称长度应为 1 到 20 个字符")
	}
	if input.Quantity < 1 || input.Quantity > 100 {
		return "", decimal.Zero, nil, gerror.New("批量数量应为 1 到 100")
	}
	if math.IsNaN(input.Amount) || math.IsInf(input.Amount, 0) || input.Amount <= 0 {
		return "", decimal.Zero, nil, gerror.New("兑换额度必须大于零")
	}
	amount := decimal.NewFromFloat(input.Amount).Round(8)
	if amount.LessThanOrEqual(decimal.Zero) {
		return "", decimal.Zero, nil, gerror.New("兑换额度精度不能小于 0.00000001")
	}
	maxAmount := decimal.NewFromInt(1_000_000_000_000).Sub(decimal.New(1, -8))
	if amount.GreaterThan(maxAmount) {
		return "", decimal.Zero, nil, gerror.New("兑换额度不能超过 999999999999.99999999")
	}
	if input.ExpiresAt == nil {
		return name, amount, nil, nil
	}
	expiresAt := input.ExpiresAt.UTC()
	if !expiresAt.After(time.Now().UTC()) {
		return "", decimal.Zero, nil, gerror.New("过期时间必须晚于当前时间")
	}
	return name, amount, &expiresAt, nil
}

func normalizeListFilter(filter ListFilter) (string, string, error) {
	status := strings.TrimSpace(filter.Status)
	switch status {
	case "", "all", statusActive, statusUsed, statusExpired:
		return strings.TrimSpace(filter.Keyword), status, nil
	default:
		return "", "", gerror.New("兑换码状态筛选无效")
	}
}

func redemptionCodeMatches(row codeRow, keyword, status string, now time.Time) bool {
	if keyword != "" {
		keyword = strings.ToLower(keyword)
		if !strings.Contains(strings.ToLower(row.Name), keyword) && !strings.Contains(strings.ToLower(row.Code), keyword) {
			return false
		}
	}
	switch status {
	case "", "all":
		return true
	case statusActive:
		return row.RedeemedAt == nil && (row.ExpiresAt == nil || row.ExpiresAt.After(now))
	case statusUsed:
		return row.RedeemedAt != nil
	case statusExpired:
		return row.RedeemedAt == nil && row.ExpiresAt != nil && !row.ExpiresAt.After(now)
	default:
		return false
	}
}

func codeStatus(row codeRow, now time.Time) string {
	if row.RedeemedAt != nil {
		return statusUsed
	}
	if row.ExpiresAt != nil && !row.ExpiresAt.After(now) {
		return statusExpired
	}
	return statusActive
}

func generateCode() (string, error) {
	bytes := make([]byte, 12)
	if _, err := cryptorand.Read(bytes); err != nil {
		return "", gerror.Wrap(err, "generate redemption code")
	}
	value := strings.ToUpper(hex.EncodeToString(bytes))
	return "AFR-" + value[0:4] + "-" + value[4:8] + "-" + value[8:12] + "-" + value[12:16] + "-" + value[16:20] + "-" + value[20:24], nil
}

func decimalFloat(value decimal.Decimal) float64 {
	result, _ := value.Float64()
	return result
}
