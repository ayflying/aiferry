package modelmetadata

import "encoding/json"

// metadataWire keeps capability flags grouped consistently for the API and storage.
type metadataWire struct {
	DisplayName      *string  `json:"display_name"`
	Description      *string  `json:"description"`
	ContextLength    *int64   `json:"context_length"`
	MaxOutputTokens  *int64   `json:"max_output_tokens"`
	InputModalities  []string `json:"input_modalities"`
	OutputModalities []string `json:"output_modalities"`
	Capabilities     struct {
		Tools            *bool `json:"tools"`
		Reasoning        *bool `json:"reasoning"`
		StructuredOutput *bool `json:"structured_output"`
	} `json:"capabilities"`
}

func (m Metadata) MarshalJSON() ([]byte, error) {
	w := metadataWire{DisplayName: m.DisplayName, Description: m.Description, ContextLength: m.ContextLength, MaxOutputTokens: m.MaxOutputTokens, InputModalities: m.InputModalities, OutputModalities: m.OutputModalities}
	w.Capabilities.Tools, w.Capabilities.Reasoning, w.Capabilities.StructuredOutput = m.Tools, m.Reasoning, m.StructuredOutput
	return json.Marshal(w)
}

func (m *Metadata) UnmarshalJSON(body []byte) error {
	var w metadataWire
	if err := json.Unmarshal(body, &w); err != nil {
		return err
	}
	*m = Metadata{DisplayName: w.DisplayName, Description: w.Description, ContextLength: w.ContextLength, MaxOutputTokens: w.MaxOutputTokens, InputModalities: w.InputModalities, OutputModalities: w.OutputModalities, Tools: w.Capabilities.Tools, Reasoning: w.Capabilities.Reasoning, StructuredOutput: w.Capabilities.StructuredOutput}
	return nil
}
