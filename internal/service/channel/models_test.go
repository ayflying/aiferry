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
