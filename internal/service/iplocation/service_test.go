package iplocation

import "testing"

func TestNormalizeIP(t *testing.T) {
	if actual := NormalizeIP("::ffff:203.0.113.8"); actual != "203.0.113.8" {
		t.Fatalf("unexpected normalized IP: %s", actual)
	}
	if actual := NormalizeIP("not-an-ip"); actual != "" {
		t.Fatalf("invalid IP should be blank: %s", actual)
	}
}

func TestFormatRegion(t *testing.T) {
	actual := formatRegion("中国|广东省|深圳市|电信|CN")
	if actual != "中国 / 广东省 / 深圳市 / 电信" {
		t.Fatalf("unexpected region: %s", actual)
	}
}
