package main

import "testing"

func TestLocalHealthURL(t *testing.T) {
	tests := []struct {
		name   string
		listen string
		want   string
	}{
		{name: "IPv4 wildcard", listen: "0.0.0.0:8787", want: "http://127.0.0.1:8787/healthz"},
		{name: "IPv4 loopback", listen: "127.0.0.1:18787", want: "http://127.0.0.1:18787/healthz"},
		{name: "IPv6 wildcard", listen: "[::]:8787", want: "http://[::1]:8787/healthz"},
		{name: "invalid fallback", listen: "invalid", want: "http://127.0.0.1:8787/healthz"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := localHealthURL(test.listen); got != test.want {
				t.Fatalf("localHealthURL(%q)=%q want %q", test.listen, got, test.want)
			}
		})
	}
}
