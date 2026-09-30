package admin

import (
	"encoding/json"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/net/ghttp"
	adminapi "github.com/yunloli/aiferry/api/admin"
)

func (c *Controller) getModelMetadata(r *ghttp.Request) {
	data, err := c.channels.GetModelMetadata(r.Context(), r.GetQuery("publicName").String())
	respond(r, data, err)
}
func (c *Controller) putModelMetadata(r *ghttp.Request) {
	var input adminapi.ModelMetadataInput
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(r.GetBody(), &fields); err != nil {
		respond(r, nil, gerror.Wrap(err, "invalid JSON body"))
		return
	}
	if _, ok := fields["metadata"]; !ok {
		respond(r, nil, gerror.New("metadata is required (use null to clear)"))
		return
	}
	if err := json.Unmarshal(r.GetBody(), &input); err != nil {
		respond(r, nil, gerror.Wrap(err, "invalid model metadata"))
		return
	}
	data, err := c.channels.PutModelMetadata(r.Context(), input.PublicName, input.Metadata)
	respond(r, data, err)
}
