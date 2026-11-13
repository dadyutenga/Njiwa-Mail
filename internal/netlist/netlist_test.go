package netlist

import (
	"net"
	"testing"
)

func TestParse(t *testing.T) {
	nets, err := Parse(" 172.30.0.1, 10.0.0.0/8 ,::1,")
	if err != nil || len(nets) != 3 {
		t.Fatalf("%v %v", nets, err)
	}
	if !Contains(nets, net.ParseIP("172.30.0.1")) || !Contains(nets, net.ParseIP("10.1.2.3")) || Contains(nets, net.ParseIP("172.30.0.2")) {
		t.Fatal("membership")
	}
	for _, bad := range []string{"mailcow-host", "10.0.0.0/33"} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}
