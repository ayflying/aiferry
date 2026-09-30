package relay

import (
	"encoding/json"
	"github.com/yunloli/aiferry/internal/logic/modelmetadata"
	"testing"
)

func TestModelJSONPreservesOpenAIFieldsAndMetadata(t *testing.T) {
	no := false
	body, err := json.Marshal(Model{ID: "alias", Object: "model", Created: 0, OwnedBy: "aiferry", Metadata: modelmetadata.Metadata{Tools: &no, InputModalities: []string{}}})
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]json.RawMessage
	if err = json.Unmarshal(body, &result); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{"id": "\"alias\"", "object": "\"model\"", "created": "0", "owned_by": "\"aiferry\""} {
		if string(result[key]) != want {
			t.Fatalf("compatibility %s: %s", key, result[key])
		}
	}
	var m modelmetadata.Metadata
	if err = json.Unmarshal(result["metadata"], &m); err != nil {
		t.Fatal(err)
	}
	if m.Tools == nil || *m.Tools || m.InputModalities == nil || m.OutputModalities != nil {
		t.Fatal("metadata null/false/empty lost")
	}
}
