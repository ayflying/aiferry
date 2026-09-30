package channel

import "testing"

func TestModelMetadataFromCustomPaths(t *testing.T) {
	models, err := modelMetadataFromJSON([]byte(`{"payload":{"models":[{"name":"same","capabilities":{"tools":true}},{"name":"same","capabilities":{"tools":false}},{"name":"unknown"}]}}`), "payload.models", "name")
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 || models["same"].Tools == nil || *models["same"].Tools || models["unknown"].Tools != nil {
		t.Fatalf("bad discovery merge: %#v", models)
	}
}
