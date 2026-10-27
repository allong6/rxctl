package engine

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/allong6/rxctl/internal/check"
	"github.com/allong6/rxctl/internal/state"
)

func sp(rel string) string { return state.Path(rel) }
func run(name string, args ...string) error {
	c := exec.Command(name, args...)
	c.Stdout, c.Stderr = os.Stdout, os.Stderr
	return c.Run()
}

func confPath(scheme string) string {
	if scheme == "tcp" {
		return sp(state.TcpDir + "/config.json")
	}
	return sp(state.UdpDir + "/config.json")
}

func unitName(scheme string) string {
	if scheme == "tcp" {
		return "reality-exit"
	}
	return "hy-exit"
}

func checkConf(scheme string) error {
	return run("sing-box", "check", "-c", confPath(scheme))
}

// RegenTcp 重建 TCP 配置并校验。
func RegenTcp() error {
	m, err := state.LoadTcpMeta()
	if err != nil {
		return err
	}
	us, err := state.LoadTcpUsers()
	if err != nil || len(us) == 0 {
		return fmt.Errorf("没有用户，无法生成配置")
	}
	if err := writeJSON(confPath("tcp"), tcpConfig(m, us)); err != nil {
		return err
	}
	return checkConf("tcp")
}

// RegenUdp 重建 UDP 配置并校验。
func RegenUdp() error {
	m, err := state.LoadUdMeta()
	if err != nil {
		return err
	}
	us, err := state.LoadUdUsers()
	if err != nil || len(us) == 0 {
		return fmt.Errorf("没有用户，无法生成配置")
	}
	if err := writeJSON(confPath("udp"), udpConfig(m, us)); err != nil {
		return err
	}
	return checkConf("udp")
}

// WriteUnit 写 systemd 单元（幂等）。
func WriteUnit(scheme string) error {
	unit := `[Unit]
Description=rxctl ` + scheme + ` exit
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=/usr/local/bin/sing-box run -c ` + confPath(scheme) + `
Restart=always
RestartSec=3
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
`
	p := sp("/etc/systemd/system/" + unitName(scheme) + ".service")
	if err := os.MkdirAll(state.DirOf(p), 0o755); err != nil {
		return err
	}
	if err := writeFileAtomic(p, []byte(unit), 0o644); err != nil {
		return err
	}
	check.Run("systemctl", "daemon-reload")
	return nil
}

// Svc 控制服务。
func Svc(action, scheme string) error {
	u := unitName(scheme)
	switch action {
	case "enable":
		return run("systemctl", "enable", "--now", u)
	case "start", "stop", "restart":
		return run("systemctl", action, u)
	case "disable":
		return run("systemctl", "disable", "--now", u)
	}
	return nil
}

// SvcActive 服务是否 active。
func SvcActive(scheme string) bool { return check.ServiceActive(unitName(scheme)) }

// SaveMeta 写 key=value 文件（600）。
func SaveMeta(path string, kv [][2]string) error {
	var b strings.Builder
	for _, p := range kv {
		fmt.Fprintf(&b, "%s=%s\n", p[0], p[1])
	}
	if err := os.MkdirAll(state.DirOf(path), 0o700); err != nil {
		return err
	}
	return writeFileAtomic(path, []byte(b.String()), 0o600)
}

// PortInUse 判断端口是否已被别的进程监听（ss 只读）。
func PortInUse(proto string, port int) bool { return check.PortListening(proto, port) }

// InstallTcp 安装 TCP 方案（VLESS + Reality）。
func InstallTcp(port int, sni, user string, quota int64, dry bool) error {
	if port <= 0 || port > 65535 {
		return fmt.Errorf("端口不合法: %d", port)
	}
	if dry {
		fmt.Printf("[dry-run] 安装 tcp：port=%d sni=%s user=%s quota=%s\n", port, sni, user, quotaWord(quota))
		fmt.Printf("[dry-run]  写 %s、生成 %s、单元 %s.service、放行/计量 TCP %d\n",
			sp(state.TcpDir+"/meta.env"), confPath("tcp"), unitName("tcp"), port)
		return nil
	}
	if _, err := exec.LookPath("sing-box"); err != nil {
		return fmt.Errorf("未找到 sing-box，请先安装（rxctl 不代下载）")
	}
	if _, err := os.Stat(sp(state.TcpDir + "/meta.env")); err != nil && PortInUse("tcp", port) {
		return fmt.Errorf("TCP %d 已被占用", port)
	}
	if err := os.MkdirAll(sp(state.TcpDir), 0o700); err != nil {
		return err
	}
	priv, pub, err := RealityKeys()
	if err != nil {
		return err
	}
	ip := publicIP()
	sid := RandHex(4)
	if err := SaveMeta(sp(state.TcpDir+"/meta.env"), [][2]string{
		{"PUB_IP", ip}, {"PORT", itoa(port)}, {"SNI", sni}, {"PRIV", priv}, {"PUB", pub}, {"SID", sid},
	}); err != nil {
		return err
	}
	uuid := check.Run("sing-box", "generate", "uuid")
	if uuid == "" {
		uuid = RandUUID()
	}
	uff := sp(state.TcpDir + "/users.tsv")
	if _, err := os.Stat(uff); err != nil {
		line := fmt.Sprintf("%s\t%s\t%d\t%d\n", user, uuid, port, quota)
		if err := writeFileAtomic(uff, []byte(line), 0o600); err != nil {
			return err
		}
	}
	if err := RegenTcp(); err != nil {
		return fmt.Errorf("配置校验失败: %w", err)
	}
	if err := WriteUnit("tcp"); err != nil {
		return err
	}
	if err := Svc("enable", "tcp"); err != nil {
		return err
	}
	FwEnsure()
	FwWatchPort("tcp", port)
	return nil
}

// InstallUdp 安装 UDP 方案（Hysteria2）。
func InstallUdp(port int, sni, user string, quota int64, dry bool) error {
	if port <= 0 || port > 65535 {
		return fmt.Errorf("端口不合法: %d", port)
	}
	if dry {
		fmt.Printf("[dry-run] 安装 udp：port=%d/udp sni=%s user=%s quota=%s\n", port, sni, user, quotaWord(quota))
		fmt.Printf("[dry-run]  自签证书 → %s、生成 %s、单元 %s.service、放行/计量 UDP %d\n",
			sp(state.UdpDir+"/cert.pem"), confPath("udp"), unitName("udp"), port)
		return nil
	}
	if _, err := exec.LookPath("sing-box"); err != nil {
		return fmt.Errorf("未找到 sing-box，请先安装")
	}
	if _, err := os.Stat(sp(state.UdpDir + "/meta.env")); err != nil && PortInUse("udp", port) {
		return fmt.Errorf("UDP %d 已被占用", port)
	}
	if err := os.MkdirAll(sp(state.UdpDir), 0o700); err != nil {
		return err
	}
	cert := sp(state.UdpDir + "/cert.pem")
	key := sp(state.UdpDir + "/key.pem")
	if _, err := os.Stat(cert); err != nil {
		if err := SelfSigned(sni, cert, key, 10); err != nil {
			return err
		}
	}
	if err := SaveMeta(sp(state.UdpDir+"/meta.env"), [][2]string{
		{"HY_SNI", sni}, {"HY_IP", publicIP()}, {"HY_PORT", itoa(port)},
		{"HY_CERT", cert}, {"HY_KEY", key},
	}); err != nil {
		return err
	}
	uff := sp(state.UdpDir + "/users.tsv")
	if _, err := os.Stat(uff); err != nil {
		line := fmt.Sprintf("%s\t%s\t%d\t%d\t\n", user, RandPassword(24), port, quota)
		if err := writeFileAtomic(uff, []byte(line), 0o600); err != nil {
			return err
		}
	}
	if err := RegenUdp(); err != nil {
		return fmt.Errorf("配置校验失败: %w", err)
	}
	if err := WriteUnit("udp"); err != nil {
		return err
	}
	if err := Svc("enable", "udp"); err != nil {
		return err
	}
	FwEnsure()
	FwWatchPort("udp", port)
	return nil
}

// Uninstall 卸载方案（保留另一套与 sing-box 二进制）。
func Uninstall(scheme string, purge bool) error {
	un := func(s string) {
		Svc("disable", s)
		os.Remove(sp("/etc/systemd/system/" + unitName(s) + ".service"))
		for _, u := range state.UsersOf(s) {
			if u.Port > 0 {
				FwUnwatchPort(s, u.Port)
			}
		}
		if purge {
			dir := state.TcpDir
			if s == "udp" {
				dir = state.UdpDir
			}
			os.RemoveAll(sp(dir))
		}
		check.Run("systemctl", "daemon-reload")
	}
	switch scheme {
	case "tcp":
		un("tcp")
	case "udp":
		un("udp")
	case "all":
		un("tcp")
		un("udp")
	default:
		return fmt.Errorf("未知方案: %s", scheme)
	}
	return nil
}

// publicIP 取本机公网 IP（失败返回空）。
func publicIP() string {
	for _, u := range []string{"https://api.ipify.org", "https://ip.sb", "https://ifconfig.me"} {
		if v := check.Run("curl", "-4", "-s", "--max-time", "8", u); v != "" && len(v) <= 45 {
			return v
		}
	}
	if out := check.Run("ip", "-4", "route", "get", "1.1.1.1"); out != "" {
		f := strings.Fields(out)
		for i := 0; i+1 < len(f); i++ {
			if f[i] == "src" {
				return f[i+1]
			}
		}
	}
	return ""
}
