package config

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"

	"github.com/gogf/gf/v2/errors/gerror"
)

// BuiltinRegistry contains the application-owned channel type definitions.
type BuiltinRegistry struct {
	ChannelTypes []BuiltinChannelType `json:"channelTypes"`
}

type BuiltinChannelType struct {
	ID     uint64          `json:"id"`
	Name   string          `json:"name"`
	Code   string          `json:"code"`
	Config json.RawMessage `json:"config"`
}

func LoadBuiltins(path string) (*BuiltinRegistry, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, gerror.Wrap(err, "read built-in configuration")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	registry := &BuiltinRegistry{}
	if err = decoder.Decode(registry); err != nil {
		return nil, gerror.Wrap(err, "decode built-in configuration")
	}
	if err = registry.validate(); err != nil {
		return nil, err
	}
	return registry, nil
}

func (r *BuiltinRegistry) ChannelTypeByID(id uint64) (BuiltinChannelType, bool) {
	for _, item := range r.ChannelTypes {
		if item.ID == id {
			return item, true
		}
	}
	return BuiltinChannelType{}, false
}

func (r *BuiltinRegistry) ChannelTypeByCode(code string) (BuiltinChannelType, bool) {
	for _, item := range r.ChannelTypes {
		if item.Code == strings.TrimSpace(code) {
			return item, true
		}
	}
	return BuiltinChannelType{}, false
}

func (r *BuiltinRegistry) validate() error {
	if len(r.ChannelTypes) == 0 {
		return gerror.New("built-in configuration must define at least one channel type")
	}
	return validateBuiltinChannelTypes(r.ChannelTypes)
}

func validateBuiltinChannelTypes(items []BuiltinChannelType) error {
	ids, codes := make(map[uint64]struct{}, len(items)), make(map[string]struct{}, len(items))
	for _, item := range items {
		if item.ID == 0 || strings.TrimSpace(item.Name) == "" || strings.TrimSpace(item.Code) == "" || !json.Valid(item.Config) {
			return gerror.New("invalid built-in channel type definition")
		}
		if _, exists := ids[item.ID]; exists {
			return gerror.Newf("duplicate built-in channel type id %d", item.ID)
		}
		if _, exists := codes[item.Code]; exists {
			return gerror.Newf("duplicate built-in channel type code %s", item.Code)
		}
		ids[item.ID], codes[item.Code] = struct{}{}, struct{}{}
	}
	return nil
}
