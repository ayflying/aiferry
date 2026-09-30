package modelmetadata

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/frame/g"
)

type Ref struct {
	ChannelID uint64
	ModelName string
}
type View struct {
	Automatic Metadata  `json:"automatic"`
	Manual    *Metadata `json:"manual"`
	Effective Metadata  `json:"effective"`
}
type Catalog map[Ref]Metadata

type stored struct {
	ChannelID uint64 `orm:"channel_id"`
	ModelName string `orm:"model_name"`
	Metadata  string `orm:"metadata"`
}

// Load batches rows for candidate channels and public override names. Resolve
// consumes only the caller's refs, never another key's routes.
func Load(ctx context.Context, channelIDs []uint64, publicNames []string) (Catalog, error) {
	result := Catalog{}
	queries := []*gdb.Model{}
	if len(channelIDs) > 0 {
		queries = append(queries, g.DB().Model("model_metadata").Ctx(ctx).WhereIn("channel_id", channelIDs))
	}
	if len(publicNames) > 0 {
		queries = append(queries, g.DB().Model("model_metadata").Ctx(ctx).Where("channel_id", 0).WhereIn("model_name", publicNames))
	}
	for _, query := range queries {
		var rows []stored
		if err := query.Scan(&rows); err != nil {
			return nil, gerror.Wrap(err, "load model metadata")
		}
		for _, row := range rows {
			var m Metadata
			if err := json.Unmarshal([]byte(row.Metadata), &m); err != nil {
				return nil, gerror.Wrap(err, "decode model metadata")
			}
			if err := Validate(m); err != nil {
				return nil, err
			}
			result[Ref{row.ChannelID, row.ModelName}] = m
		}
	}
	return result, nil
}
func (c Catalog) Resolve(publicName string, refs []Ref) View {
	values := make([]Metadata, 0, len(refs))
	seen := map[Ref]bool{}
	for _, ref := range refs {
		if ref.ChannelID == 0 || seen[ref] {
			continue
		}
		seen[ref] = true
		values = append(values, c[ref])
	}
	automatic := Aggregate(values)
	var manual *Metadata
	if m, ok := c[Ref{0, publicName}]; ok {
		manual = &m
	}
	return View{Automatic: automatic, Manual: manual, Effective: Merge(automatic, manual)}
}

func SaveUpstream(ctx context.Context, channelID uint64, values map[string]Metadata) error {
	if channelID == 0 {
		return gerror.New("upstream channel_id must be positive")
	}
	return g.DB().Transaction(ctx, func(txCtx context.Context, _ gdb.TX) error {
		for name, m := range values {
			if err := validateName(name); err != nil {
				return err
			}
			if err := save(txCtx, channelID, name, m); err != nil {
				return err
			}
		}
		return nil
	})
}
func PutManual(ctx context.Context, publicName string, metadata *Metadata) error {
	if err := validateName(publicName); err != nil {
		return err
	}
	if metadata == nil {
		_, err := g.DB().Model("model_metadata").Ctx(ctx).Where("channel_id", 0).Where("model_name", publicName).Delete()
		return gerror.Wrap(err, "clear model metadata override")
	}
	return save(ctx, 0, publicName, *metadata)
}
func save(ctx context.Context, channelID uint64, name string, m Metadata) error {
	if err := Validate(m); err != nil {
		return err
	}
	body, err := json.Marshal(m)
	if err != nil {
		return gerror.Wrap(err, "encode model metadata")
	}
	data := stored{ChannelID: channelID, ModelName: name, Metadata: string(body)}
	_, err = g.DB().Model("model_metadata").Ctx(ctx).Data(data).OnDuplicate("metadata").Save()
	return gerror.Wrap(err, "save model metadata")
}
func validateName(name string) error {
	if strings.TrimSpace(name) == "" || len(name) > 191 {
		return gerror.New("model name must be nonempty and at most 191 bytes")
	}
	return nil
}

// Get aggregates all enabled configured routes for administrator editing. Public
// relay callers must instead Load/Resolve with their filtered route candidates.
func Get(ctx context.Context, publicName string) (View, error) {
	if err := validateName(publicName); err != nil {
		return View{}, err
	}
	var rows []struct {
		ChannelID    uint64 `orm:"channel_id"`
		UpstreamName string `orm:"upstream_name"`
	}
	if err := g.DB().Model("channel_models m").Ctx(ctx).
		LeftJoin("channels c", "c.id=m.channel_id").
		Fields("m.channel_id,m.upstream_name").Where("m.public_name", publicName).
		Where("m.enabled", 1).Where("c.status", 1).WhereNull("m.auto_disabled_at").Scan(&rows); err != nil {
		return View{}, gerror.Wrap(err, "load model metadata routes")
	}
	ids := make([]uint64, 0, len(rows))
	refs := make([]Ref, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ChannelID)
		refs = append(refs, Ref{row.ChannelID, row.UpstreamName})
	}
	catalog, err := Load(ctx, ids, []string{publicName})
	if err != nil {
		return View{}, err
	}
	return catalog.Resolve(publicName, refs), nil
}
