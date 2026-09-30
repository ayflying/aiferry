package modelmetadata

import (
	"encoding/json"
	"testing"
)

func TestMetadataWireCapabilitiesAndFalse(t *testing.T) {
	var m Metadata
	if err := json.Unmarshal([]byte(`{"input_modalities":[],"capabilities":{"tools":false,"reasoning":true,"structured_output":null}}`), &m); err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]json.RawMessage
	if err = json.Unmarshal(body, &wire); err != nil {
		t.Fatal(err)
	}
	if _, exists := wire["tools"]; exists {
		t.Fatal("capabilities must not be flattened")
	}
	var flags map[string]json.RawMessage
	if err = json.Unmarshal(wire["capabilities"], &flags); err != nil {
		t.Fatal(err)
	}
	if string(flags["tools"]) != "false" || string(flags["reasoning"]) != "true" || string(flags["structured_output"]) != "null" {
		t.Fatalf("wrong capabilities: %s", body)
	}
	if string(wire["input_modalities"]) != "[]" || string(wire["output_modalities"]) != "null" {
		t.Fatalf("wrong modality states: %s", body)
	}
}
