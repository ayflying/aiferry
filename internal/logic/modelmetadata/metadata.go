package modelmetadata

import (
	"github.com/gogf/gf/v2/errors/gerror"
	"sort"
	"unicode/utf8"
)

// Null means unknown/inherit; empty modalities and false are explicit overrides.
type Metadata struct {
	DisplayName      *string  `json:"display_name"`
	Description      *string  `json:"description"`
	ContextLength    *int64   `json:"context_length"`
	MaxOutputTokens  *int64   `json:"max_output_tokens"`
	InputModalities  []string `json:"input_modalities"`
	OutputModalities []string `json:"output_modalities"`
	Tools            *bool    `json:"-"`
	Reasoning        *bool    `json:"reasoning"`
	StructuredOutput *bool    `json:"structured_output"`
}

func Validate(m Metadata) error {
	if m.DisplayName != nil && utf8.RuneCountInString(*m.DisplayName) > 191 {
		return gerror.New("display_name is too long")
	}
	if m.Description != nil && utf8.RuneCountInString(*m.Description) > 8192 {
		return gerror.New("description is too long")
	}
	for _, n := range []*int64{m.ContextLength, m.MaxOutputTokens} {
		if n != nil && (*n <= 0 || *n > 1<<53-1) {
			return gerror.New("token lengths must be positive safe integers")
		}
	}
	for _, modalities := range [][]string{m.InputModalities, m.OutputModalities} {
		seen := map[string]bool{}
		for _, value := range modalities {
			switch value {
			case "text", "image", "audio", "video", "file":
			default:
				return gerror.Newf("invalid modality: %s", value)
			}
			if seen[value] {
				return gerror.Newf("duplicate modality: %s", value)
			}
			seen[value] = true
		}
	}
	return nil
}
func Merge(a Metadata, manual *Metadata) Metadata {
	if manual == nil {
		return a
	}
	m := *manual
	if m.DisplayName != nil {
		a.DisplayName = m.DisplayName
	}
	if m.Description != nil {
		a.Description = m.Description
	}
	if m.ContextLength != nil {
		a.ContextLength = m.ContextLength
	}
	if m.MaxOutputTokens != nil {
		a.MaxOutputTokens = m.MaxOutputTokens
	}
	if m.InputModalities != nil {
		a.InputModalities = m.InputModalities
	}
	if m.OutputModalities != nil {
		a.OutputModalities = m.OutputModalities
	}
	if m.Tools != nil {
		a.Tools = m.Tools
	}
	if m.Reasoning != nil {
		a.Reasoning = m.Reasoning
	}
	if m.StructuredOutput != nil {
		a.StructuredOutput = m.StructuredOutput
	}
	return a
}

// Aggregate only promises common support across every candidate; unknown propagates.
func Aggregate(values []Metadata) Metadata {
	if len(values) == 0 {
		return Metadata{}
	}
	m := values[0]
	for _, v := range values[1:] {
		m.DisplayName = sameText(m.DisplayName, v.DisplayName)
		m.Description = sameText(m.Description, v.Description)
		m.ContextLength = minimum(m.ContextLength, v.ContextLength)
		m.MaxOutputTokens = minimum(m.MaxOutputTokens, v.MaxOutputTokens)
		m.InputModalities = intersection(m.InputModalities, v.InputModalities)
		m.OutputModalities = intersection(m.OutputModalities, v.OutputModalities)
		m.Tools = commonBool(m.Tools, v.Tools)
		m.Reasoning = commonBool(m.Reasoning, v.Reasoning)
		m.StructuredOutput = commonBool(m.StructuredOutput, v.StructuredOutput)
	}
	return m
}
func sameText(a, b *string) *string {
	if a == nil || b == nil || *a != *b {
		return nil
	}
	return a
}
func minimum(a, b *int64) *int64 {
	if a == nil || b == nil {
		return nil
	}
	n := min(*a, *b)
	return &n
}
func commonBool(a, b *bool) *bool {
	if a == nil || b == nil {
		return nil
	}
	v := *a && *b
	return &v
}
func intersection(a, b []string) []string {
	if a == nil || b == nil {
		return nil
	}
	set := map[string]bool{}
	for _, value := range b {
		set[value] = true
	}
	result := make([]string, 0)
	for _, value := range a {
		if set[value] {
			result = append(result, value)
		}
	}
	sort.Strings(result)
	return result
}
