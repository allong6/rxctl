package engine

import (
	"reflect"
	"testing"
)

func TestParseSecurePathDebian(t *testing.T) {
	out := "Sudo version 1.9.5p2\nMatch group sudo:\n    secure_path=\"/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin\"\n"
	got := parseSecurePath(out)
	want := []string{"/usr/local/sbin", "/usr/local/bin", "/usr/sbin", "/usr/bin", "/sbin", "/bin"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestParseSecurePathCentOS(t *testing.T) {
	out := "Sudo version 1.8.29\nValue to override user's $PATH with: /sbin:/bin:/usr/sbin:/usr/bin\n"
	got := parseSecurePath(out)
	want := []string{"/sbin", "/bin", "/usr/sbin", "/usr/bin"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestParseSecurePathMissing(t *testing.T) {
	if got := parseSecurePath("Sudo version 1.9.5p2\n"); got != nil {
		t.Fatalf("want nil, got %v", got)
	}
}
