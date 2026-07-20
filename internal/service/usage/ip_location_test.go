package usage

import "testing"

type locationStub struct {
	values map[string]string
	calls  map[string]int
}

func (s *locationStub) Lookup(ip string) string {
	s.calls[ip]++
	return s.values[ip]
}

func TestPopulateIPLocationsUsesLookupOncePerIP(t *testing.T) {
	locator := &locationStub{
		values: map[string]string{"203.0.113.8": "测试地区"},
		calls:  make(map[string]int),
	}
	service := New(locator)
	items := []LogView{
		{ClientIP: "203.0.113.8"},
		{ClientIP: "203.0.113.8"},
		{ClientIP: "198.51.100.1", IPLocation: "已记录地区"},
	}

	service.populateIPLocations(items)

	if items[0].IPLocation != "测试地区" || items[1].IPLocation != "测试地区" {
		t.Fatalf("missing resolved location: %#v", items)
	}
	if items[2].IPLocation != "已记录地区" {
		t.Fatalf("recorded location should not be replaced: %s", items[2].IPLocation)
	}
	if locator.calls["203.0.113.8"] != 1 || locator.calls["198.51.100.1"] != 0 {
		t.Fatalf("unexpected lookup calls: %#v", locator.calls)
	}
}

func TestResolveIPLocationPreservesRecordedValue(t *testing.T) {
	service := New(&locationStub{calls: make(map[string]int)})
	if actual := service.resolveIPLocation("203.0.113.8", "已记录地区"); actual != "已记录地区" {
		t.Fatalf("unexpected recorded location: %s", actual)
	}
}
