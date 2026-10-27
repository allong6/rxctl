//go:build !windows

package state

import (
	"os"
	"path/filepath"
	"testing"
)

// 非 root 读 700 目录时必须能识别为"权限不足"，而不是"未安装"。
func TestPermissionDenied(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root 不受文件权限限制，跳过")
	}
	old := Root
	defer func() { Root = old }()

	dir := t.TempDir()
	Root = dir
	tcp := filepath.Join(dir, "etc", "reality-exit")
	if err := os.MkdirAll(tcp, 0o700); err != nil {
		t.Fatal(err)
	}
	if PermissionDenied() {
		t.Fatal("目录可读时不应报权限不足")
	}
	if err := os.Chmod(tcp, 0o000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(tcp, 0o700)
	if !PermissionDenied() {
		t.Fatal("700→000 后应识别为权限不足")
	}
}
