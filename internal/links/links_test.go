package links

import (
	"strings"
	"testing"

	"rxctl/internal/state"
)

func TestVless(t *testing.T) {
	m := state.TcpMeta{PubIP: "1.2.3.4", Port: 443, SNI: "www.microsoft.com", Pub: "PUBKEY", SID: "abcd"}
	u := state.TcpUser{Name: "alice", UUID: "uuid-1"}
	got := Vless(m, u)
	for _, want := range []string{"vless://uuid-1@1.2.3.4:443?", "security=reality", "pbk=PUBKEY", "sid=abcd", "#alice"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %s", want, got)
		}
	}
	u.Port = 20001
	if !strings.Contains(Vless(m, u), ":20001?") {
		t.Fatalf("per-user port not used: %s", Vless(m, u))
	}
}

func TestHysteria2(t *testing.T) {
	m := state.UdMeta{SNI: "www.microsoft.com"}
	u := state.UdUser{Name: "bob", Password: "pw", Port: 24446, Obfs: "ob"}
	got := Hysteria2("1.2.3.4", m, u)
	for _, want := range []string{"hysteria2://pw@1.2.3.4:24446/", "insecure=1", "sni=www.microsoft.com", "obfs=salamander", "obfs-password=ob"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %s", want, got)
		}
	}
}
