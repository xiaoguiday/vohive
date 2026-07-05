package voiceclient

import "testing"

func TestNormalizeDNSServerAddrs(t *testing.T) {
	got := normalizeDNSServerAddrs([]string{
		"10.0.0.53",
		"10.0.0.53:5353",
		"[2001:db8::53]",
		"[2001:db8::54]:5353",
		"10.0.0.53",
		"",
	})
	want := []string{
		"10.0.0.53:53",
		"10.0.0.53:5353",
		"[2001:db8::53]:53",
		"[2001:db8::54]:5353",
	}
	if len(got) != len(want) {
		t.Fatalf("addrs=%+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("addrs[%d]=%q, want %q (all=%+v)", i, got[i], want[i], got)
		}
	}
}

func TestParseSIPURIEndpointDefaultsSIPSPort(t *testing.T) {
	endpoint, err := parseSIPURIEndpoint("sips:user@ims.example;transport=tcp")
	if err != nil {
		t.Fatalf("parseSIPURIEndpoint() error = %v", err)
	}
	if endpoint.addr() != "ims.example:5061" || !endpoint.Secure || endpoint.ExplicitPort {
		t.Fatalf("endpoint=%+v addr=%q", endpoint, endpoint.addr())
	}
}
