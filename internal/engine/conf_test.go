package engine

import (
	"testing"

	"github.com/allong6/rxctl/internal/state"
)

func TestTcpInboundsGrouping(t *testing.T) {
	m := state.TcpMeta{PubIP: "1.2.3.4", Port: 443, SNI: "www.microsoft.com", Priv: "priv", Pub: "pub", SID: "abcd"}
	us := []state.TcpUser{
		{Name: "alice", UUID: "u1", Port: 443},
		{Name: "bob", UUID: "u2", Port: 8443},
		{Name: "carol", UUID: "u3", Port: 0}, // 走默认端口
	}
	ins := tcpInbounds(m, us)
	if len(ins) != 2 {
		t.Fatalf("want 2 inbounds, got %d", len(ins))
	}
	first := ins[0].(map[string]any)
	if first["listen_port"].(int) != 443 {
		t.Fatalf("first port: %+v", first)
	}
	if n := len(first["users"].([]any)); n != 2 {
		t.Fatalf("default-port users should group together, got %d", n)
	}
	second := ins[1].(map[string]any)
	if second["listen_port"].(int) != 8443 {
		t.Fatalf("second port: %+v", second)
	}
}

func TestUdpConfigPerUser(t *testing.T) {
	m := state.UdMeta{SNI: "www.microsoft.com", Cert: "/c.pem", Key: "/k.pem"}
	us := []state.UdUser{
		{Name: "a", Password: "p1", Port: 24445},
		{Name: "b", Password: "p2", Port: 24446, Obfs: "ob"},
	}
	cfg := udpConfig(m, us)
	ins := cfg["inbounds"].([]any)
	if len(ins) != 2 {
		t.Fatalf("want 2 inbounds, got %d", len(ins))
	}
	if _, ok := ins[1].(map[string]any)["obfs"]; !ok {
		t.Fatalf("obfs missing: %+v", ins[1])
	}
}
