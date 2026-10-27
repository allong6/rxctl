// Package check 提供只读探测：服务状态、端口监听、外部命令、系统信息。
package check

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

// Run 执行命令并返回去空白输出。
func Run(name string, args ...string) string {
	out, err := exec.Command(name, args...).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// OK 执行命令并返回是否成功（用于 iptables -C/-D 这类"无输出但有退出码"的判断）。
func OK(name string, args ...string) bool {
	return exec.Command(name, args...).Run() == nil
}

// Has 判断命令是否存在。
func Has(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// ServiceActive 返回 systemd 单元是否 active。
func ServiceActive(unit string) bool {
	return Run("systemctl", "is-active", unit) == "active"
}

// PortListening 探测本机端口是否有监听。
// 只在 Linux 上可信（用 ss 读内核表）；其他平台返回 false 并记 unknown。
func PortListening(proto string, port int) bool {
	if port <= 0 || runtime.GOOS != "linux" {
		return false
	}
	flag := "-tln"
	if strings.HasPrefix(strings.ToLower(proto), "u") {
		flag = "-uln"
	}
	out := Run("ss", flag)
	if out == "" {
		return false
	}
	return strings.Contains(out, ":"+strconv.Itoa(port)+" ")
}

// FileMode 返回文件权限（四位八进制），读不到返回空串。
func FileMode(p string) string {
	fi, err := os.Stat(p)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%04o", fi.Mode().Perm())
}

// SingboxVersion 返回 sing-box 版本与路径。
func SingboxVersion() (ver, path string) {
	path = "/usr/local/bin/sing-box"
	if !Has(path) {
		path = Run("sh", "-c", "command -v sing-box")
	}
	if path == "" {
		return "", ""
	}
	out := Run(path, "version")
	// 形如: sing-box version 1.14.3
	if f := strings.Fields(out); len(f) >= 3 {
		ver = f[2]
	}
	return ver, path
}

// OSRelease 返回 PRETTY_NAME。
func OSRelease() string {
	out := Run("sh", "-c", ". /etc/os-release 2>/dev/null && echo $PRETTY_NAME")
	return out
}

// Kernel 返回内核版本。
func Kernel() string { return Run("uname", "-r") }

// SystemdVersion 返回 systemd 主版本号。
func SystemdVersion() string {
	out := Run("systemctl", "--version")
	if i := strings.Index(out, "systemd "); i >= 0 {
		rest := out[i+8:]
		if j := strings.IndexAny(rest, " \n("); j > 0 {
			return rest[:j]
		}
	}
	return ""
}
