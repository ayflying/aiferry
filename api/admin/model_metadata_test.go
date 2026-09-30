package admin

import (
	"encoding/json"
	"testing"
)

func TestModelMetadataInputNullAndEmpty(t *testing.T) {
	var cleared ModelMetadataInput
	if err := json.Unmarshal([]byte(`{"publicName":"alias","metadata":null}`), &cleared); err != nil {
		t.Fatal(err)
	}
	if cleared.Metadata != nil {
		t.Fatal("null must clear document")
	}
	var override ModelMetadataInput
	if err := json.Unmarshal([]byte(`{"publicName":"alias","metadata":{"capabilities":{"tools":false},"input_modalities":[],"output_modalities":null}}`), &override); err != nil {
		t.Fatal(err)
	}
	if override.Metadata == nil || override.Metadata.Tools == nil || *override.Metadata.Tools || override.Metadata.InputModalities == nil || override.Metadata.OutputModalities != nil {
		t.Fatal("override three-state semantics lost")
	}
}
