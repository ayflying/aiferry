package channel

import (
	"reflect"
	"testing"
)

func TestNormalizeModelNamesSortsAndDeduplicates(t *testing.T) {
	input := []string{" zeta ", "Alpha", "beta", "Alpha", "", "alpha"}
	want := []string{"Alpha", "alpha", "beta", "zeta"}

	if got := normalizeModelNames(input); !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected model names: got %v, want %v", got, want)
	}
}

func TestModelNamesFromCustomJSONPaths(t *testing.T) {
	names, err := modelNamesFromJSON([]byte(`{"payload":{"models":[{"name":"zeta"},{"name":"alpha"},{"name":"alpha"}]}}`), "payload.models", "name")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"alpha", "zeta"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("unexpected names: got %v, want %v", names, want)
	}
}

func TestModelNamesRejectsNonArrayPath(t *testing.T) {
	if _, err := modelNamesFromJSON([]byte(`{"payload":{"model":"gpt"}}`), "payload.model", "id"); err == nil {
		t.Fatal("expected non-array model path to fail")
	}
}
