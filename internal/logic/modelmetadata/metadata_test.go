package modelmetadata

import (
	"encoding/json"
	"github.com/tidwall/gjson"
	"reflect"
	"testing"
)

func ptr[T any](v T) *T { return &v }
func TestParseExplicitFactsOnly(t *testing.T) {
	m := Parse(gjson.Parse(`{"id":"vision-reasoning-tools","architecture":{"input_modalities":["text","image"],"output_modalities":[]},"context_length":128000,"max_output_tokens":4096,"capabilities":{"tools":false,"reasoning":true,"structured_output":true}}`))
	if m.Tools == nil || *m.Tools || m.Reasoning == nil || !*m.Reasoning || m.ContextLength == nil || *m.ContextLength != 128000 || !reflect.DeepEqual(m.OutputModalities, []string{}) {
		t.Fatalf("bad explicit metadata: %#v", m)
	}
	unknown := Parse(gjson.Parse(`{"id":"vision-reasoning-tools","tools":"true","context_length":-1,"input_modalities":["magic"],"output_modalities":[null]}`))
	if unknown.Tools != nil || unknown.Reasoning != nil || unknown.ContextLength != nil || unknown.InputModalities != nil || unknown.OutputModalities != nil {
		t.Fatalf("inferred or invalid facts: %#v", unknown)
	}
}
func TestAggregateConservativeAndManual(t *testing.T) {
	a := Metadata{Tools: ptr(true), ContextLength: ptr(int64(100)), InputModalities: []string{"text", "image"}}
	b := Metadata{Tools: ptr(false), ContextLength: ptr(int64(50)), InputModalities: []string{"text"}}
	m := Aggregate([]Metadata{a, b})
	if m.Tools == nil || *m.Tools || *m.ContextLength != 50 || !reflect.DeepEqual(m.InputModalities, []string{"text"}) {
		t.Fatalf("not conservative: %#v", m)
	}
	unknown := Aggregate([]Metadata{a, {}})
	if unknown.Tools != nil || unknown.ContextLength != nil || unknown.InputModalities != nil {
		t.Fatal("unknown must propagate")
	}
	manual := Metadata{Tools: ptr(false), InputModalities: []string{}}
	merged := Merge(a, &manual)
	if *merged.Tools || merged.InputModalities == nil || len(merged.InputModalities) != 0 || *merged.ContextLength != 100 {
		t.Fatal("false/empty overrides or inheritance lost")
	}
	if !reflect.DeepEqual(Merge(a, nil), a) {
		t.Fatal("cleared override changed automatic facts")
	}
}
func TestMetadataNullJSONRoundTrip(t *testing.T) {
	var m Metadata
	if err := json.Unmarshal([]byte(`{"capabilities":{"tools":false,"reasoning":null},"input_modalities":[],"output_modalities":null}`), &m); err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	var again Metadata
	if err = json.Unmarshal(body, &again); err != nil {
		t.Fatal(err)
	}
	if again.Tools == nil || *again.Tools || again.Reasoning != nil || again.InputModalities == nil || again.OutputModalities != nil {
		t.Fatalf("null semantics lost: %s", body)
	}
}
func TestResolveOnlyProvidedCandidates(t *testing.T) {
	catalog := Catalog{Ref{1, "up"}: {Tools: ptr(true)}, Ref{2, "up"}: {Tools: ptr(false)}}
	view := catalog.Resolve("alias", []Ref{{1, "up"}})
	if view.Effective.Tools == nil || !*view.Effective.Tools {
		t.Fatal("invisible route affected result")
	}
	view = catalog.Resolve("alias", []Ref{{1, "up"}, {3, "missing"}})
	if view.Effective.Tools != nil {
		t.Fatal("missing route facts must be unknown")
	}
	catalog[Ref{0, "alias"}] = Metadata{Tools: ptr(false)}
	view = catalog.Resolve("alias", []Ref{{1, "up"}})
	if view.Manual == nil || *view.Effective.Tools {
		t.Fatal("public-name override did not win")
	}
}
func TestValidateRejectsInvalidLengthsAndModalities(t *testing.T) {
	for _, m := range []Metadata{{ContextLength: ptr(int64(0))}, {MaxOutputTokens: ptr(int64(-1))}, {InputModalities: []string{"text", "text"}}, {OutputModalities: []string{"invalid"}}} {
		if Validate(m) == nil {
			t.Fatalf("accepted %#v", m)
		}
	}
}
