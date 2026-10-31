package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParsePortMap(t *testing.T) {
	m, err := parsePortMap("24443=28544, 24444=28545")
	if err != nil {
		t.Fatal(err)
	}
	if m[24443] != 28544 || m[24444] != 28545 {
		t.Fatalf("bad map: %#v", m)
	}
	if _, err := parsePortMap("24443"); err == nil {
		t.Fatal("want error for missing =")
	}
	if _, err := parsePortMap("0=1"); err == nil {
		t.Fatal("want error for invalid port")
	}
}

func testNodes() []exportNode {
	return []exportNode{
		{Name: "office", Scheme: "tcp", Server: "203.0.113.10", Port: 443,
			UUID: "uuid-1", SNI: "www.microsoft.com", Pub: "pubkey", SID: "abcd1234"},
		{Name: "mobile", Scheme: "udp", Server: "203.0.113.10", Port: 8443,
			Password: "secret", SNI: "www.microsoft.com", Obfs: "obfspass"},
	}
}

func TestRenderClash(t *testing.T) {
	y := renderClash(testNodes(), true)
	for _, want := range []string{
		"mixed-port: 7890",
		"type: vless",
		"type: hysteria2",
		"reality-opts:",
		"public-key: \"pubkey\"",
		"obfs: salamander",
		"GEOSITE,cn,DIRECT",
		"GEOIP,CN,DIRECT",
		"MATCH,PROXY",
		"name: PROXY",
	} {
		if !strings.Contains(y, want) {
			t.Fatalf("clash output missing %q\n%s", want, y)
		}
	}
}

func TestRenderClashNoRules(t *testing.T) {
	y := renderClash(testNodes(), false)
	if strings.Contains(y, "GEOSITE,cn,DIRECT") {
		t.Fatalf("rules should be omitted:\n%s", y)
	}
	if !strings.Contains(y, "MATCH,PROXY") {
		t.Fatalf("missing MATCH rule:\n%s", y)
	}
}

func TestRenderSingbox(t *testing.T) {
	s, err := renderSingbox(testNodes(), true)
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := json.Unmarshal([]byte(s), &cfg); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	obs := cfg["outbounds"].([]any)
	if len(obs) != 4 { // 2 proxies + selector + direct
		t.Fatalf("want 4 outbounds, got %d", len(obs))
	}
	route := cfg["route"].(map[string]any)
	if route["final"] != "PROXY" {
		t.Fatalf("final = %v", route["final"])
	}
	if _, ok := route["rule_set"]; !ok {
		t.Fatal("rule_set missing")
	}
	first := obs[0].(map[string]any)
	if first["type"] != "vless" {
		t.Fatalf("first outbound = %v", first["type"])
	}
}

func TestExportNodeURI(t *testing.T) {
	n := exportNode{Name: "office", Scheme: "tcp", Server: "203.0.113.10", Port: 443,
		UUID: "uuid-1", SNI: "www.microsoft.com", Pub: "pub", SID: "sid"}
	u := n.uri()
	if !strings.HasPrefix(u, "vless://uuid-1@203.0.113.10:443?") {
		t.Fatalf("unexpected uri: %s", u)
	}
	n2 := exportNode{Name: "phone", Scheme: "udp", Server: "203.0.113.10", Port: 8443, Password: "pw", SNI: "sni"}
	if u2 := n2.uri(); !strings.HasPrefix(u2, "hysteria2://pw@203.0.113.10:8443/") {
		t.Fatalf("unexpected uri: %s", u2)
	}
}
