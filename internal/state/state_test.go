package state

import (
	"os"
	"path/filepath"
	"testing"
)

func mk(t *testing.T, rel, body string) {
	t.Helper()
	p := filepath.Join(Root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLoadTcpUsers(t *testing.T) {
	Root = t.TempDir()
	mk(t, TcpDir+"/users.tsv", "alice\tuuid-a\n"+"bob\tuuid-b\t20001\t536870912000\n")
	us, err := LoadTcpUsers()
	if err != nil {
		t.Fatal(err)
	}
	if len(us) != 2 || us[0].Name != "alice" || us[0].Port != 0 {
		t.Fatalf("unexpected: %+v", us)
	}
	if us[1].Port != 20001 || us[1].Quota != 536870912000 {
		t.Fatalf("unexpected bob: %+v", us[1])
	}
}

func TestLoadUdUsersLegacy(t *testing.T) {
	Root = t.TempDir()
	mk(t, UdpDir+"/meta.env", "HY_PORT=24446\nHY_SNI=www.microsoft.com\nHY_PASS=secret\n")
	us, err := LoadUdUsers()
	if err != nil {
		t.Fatal(err)
	}
	if len(us) != 1 || us[0].Name != "default" || us[0].Password != "secret" || us[0].Port != 24446 {
		t.Fatalf("unexpected: %+v", us)
	}
}

func TestParseSize(t *testing.T) {
	cases := map[string]int64{
		"":      0,
		"0":     0,
		"none":  0,
		"500G":  500 << 30,
		"100M":  100 << 20,
		"1T":    1 << 40,
		"12345": 12345,
	}
	for in, want := range cases {
		got, err := ParseSize(in)
		if err != nil || got != want {
			t.Fatalf("ParseSize(%q)=%d,%v want %d", in, got, err, want)
		}
	}
}

func TestHuman(t *testing.T) {
	if Human(512) != "512B" || Human(1536) != "1.5K" {
		t.Fatalf("Human wrong: %s %s", Human(512), Human(1536))
	}
}
