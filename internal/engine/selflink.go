package engine

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/allong6/rxctl/internal/check"
)

// LinkResult 描述 sudo 入口软链的处理结果。
type LinkResult struct {
	Created bool
	Path    string
	Note    string
}

var reSecurePath = regexp.MustCompile(`(?i)secure_path="([^"]+)"|PATH with:\s*(\S+)`)

// parseSecurePath 从 `sudo -V` 输出里解析 secure_path。
// 兼容两种格式：
//
//	Debian:  secure_path="/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
//	CentOS:  Value to override user's $PATH with: /sbin:/bin:/usr/sbin:/usr/bin
func parseSecurePath(out string) []string {
	m := reSecurePath.FindStringSubmatch(out)
	if m == nil {
		return nil
	}
	s := m[1]
	if s == "" {
		s = m[2]
	}
	var dirs []string
	for _, d := range strings.Split(s, ":") {
		d = path.Clean(d)
		if d != "" && path.IsAbs(d) {
			dirs = append(dirs, d)
		}
	}
	return dirs
}

func resolvesTo(path, exe string) bool {
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return false
	}
	want, err := filepath.EvalSymlinks(exe)
	if err != nil {
		want = exe
	}
	return filepath.Clean(real) == filepath.Clean(want)
}

// EnsureSelfLink 让 `sudo rxctl` 可用：
//  1. 先用 `sudo -n sh -c 'command -v rxctl'` 做行为判断（最准）；
//  2. 找不到时才解析 sudo 的 secure_path，挑一个可写目录建软链；
//  3. 已存在同目标软链则跳过；普通文件/指向别处的软链绝不覆盖。
//
// 只读判断 + 最小改动，失败只返回说明，不影响安装本身。
func EnsureSelfLink(exe string) LinkResult {
	if runtime.GOOS != "linux" {
		return LinkResult{Note: "非 Linux，跳过 sudo 入口"}
	}
	if !check.Has("sudo") {
		return LinkResult{Note: "未安装 sudo，跳过 sudo 入口"}
	}

	if p := check.Run("sudo", "-n", "sh", "-c", "command -v rxctl"); p != "" {
		if resolvesTo(p, exe) {
			return LinkResult{Path: p, Note: "sudo 已能找到 rxctl"}
		}
		return LinkResult{Note: fmt.Sprintf("sudo 找到的是 %s（不是本二进制），未改动", p)}
	}

	dirs := parseSecurePath(check.Run("sudo", "-V"))
	if len(dirs) == 0 {
		return LinkResult{Note: "读不到 sudo 的 secure_path，跳过（可用绝对路径 sudo " + exe + "）"}
	}
	for _, d := range dirs {
		st, err := os.Stat(d)
		if err != nil || !st.IsDir() {
			continue
		}
		target := filepath.Join(d, "rxctl")
		if fi, err := os.Lstat(target); err == nil {
			if fi.Mode()&os.ModeSymlink != 0 && resolvesTo(target, exe) {
				return LinkResult{Path: target, Note: "sudo 入口已存在"}
			}
			continue // 普通文件或指向别处：不覆盖，换下一个目录
		}
		if err := os.Symlink(exe, target); err == nil {
			return LinkResult{Created: true, Path: target, Note: "已创建 sudo 入口"}
		}
	}
	return LinkResult{Note: "没有可写的 secure_path 目录；可手工: sudo ln -s " + exe + " /usr/bin/rxctl"}
}

// RemoveSelfLink 只删除“指向本二进制”的软链，绝不碰普通文件或别人的软链。
func RemoveSelfLink(exe string) LinkResult {
	for _, d := range []string{"/usr/bin", "/bin"} {
		target := filepath.Join(d, "rxctl")
		fi, err := os.Lstat(target)
		if err != nil || fi.Mode()&os.ModeSymlink == 0 {
			continue
		}
		if resolvesTo(target, exe) {
			if err := os.Remove(target); err == nil {
				return LinkResult{Path: target, Note: "已移除 sudo 入口"}
			}
		}
	}
	return LinkResult{}
}
