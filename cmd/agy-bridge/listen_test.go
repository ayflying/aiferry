package main

import "testing"

func TestAllowedListen(t *testing.T) {
	for _, tc := range []struct {
		addr          string
		private, want bool
	}{
		{"127.0.0.1:8091", false, true},
		{"[::1]:8091", false, true},
		{"0.0.0.0:8091", false, false},
		{"0.0.0.0:8091", true, true},
		{"[::]:8091", true, false},
		{":8091", true, false},
		{"localhost:8091", true, false},
	} {
		if got := allowedListen(tc.addr, tc.private); got != tc.want {
			t.Errorf("allowedListen(%q, %t) = %t; want %t", tc.addr, tc.private, got, tc.want)
		}
	}
}
