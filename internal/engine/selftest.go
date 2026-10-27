package engine

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"time"

	"github.com/allong6/rxctl/internal/check"
	"github.com/allong6/rxctl/internal/state"
)

// SelfTestResult 是一次真实出网自检的结果。
type SelfTestResult struct {
	Scheme string `json:"scheme"`
	OK     bool   `json:"ok"`
	Port   int    `json:"port"`
	ExitIP string `json:"exit_ip"`
	Want   string `json:"want"`
	Note   string `json:"note"`
}

// SelfTest 在本机起一个临时 sing-box 客户端，经用户自己的链接出网，比对出口 IP。
// 只写 /tmp 下的临时文件，跑完即删；不改服务、不改配置。
func SelfTest(scheme string) []SelfTestResult {
	switch scheme {
	case "tcp":
		return []SelfTestResult{tcpSelfTest()}
	case "udp":
		return []SelfTestResult{udpSelfTest()}
	default:
		return []SelfTestResult{tcpSelfTest(), udpSelfTest()}
	}
}

func tcpSelfTest() SelfTestResult {
	res := SelfTestResult{Scheme: "tcp"}
	if !state.TcpInstalled() {
		res.OK, res.Note = true, "未安装"
		return res
	}
	m, err := state.LoadTcpMeta()
	if err != nil {
		res.Note = "读 meta 失败: " + err.Error()
		return res
	}
	us, _ := state.LoadTcpUsers()
	if len(us) == 0 {
		res.Note = "没有用户"
		return res
	}
	u := us[0]
	port := u.Port
	if port == 0 {
		port = m.Port
	}
	res.Port = port
	res.Want = m.PubIP

	socksPort, err := freePort()
	if err != nil {
		res.Note = err.Error()
		return res
	}
	cfg := map[string]any{
		"log": map[string]any{"level": "error"},
		"inbounds": []any{map[string]any{
			"type": "mixed", "tag": "in", "listen": "127.0.0.1", "listen_port": socksPort,
		}},
		"outbounds": []any{map[string]any{
			"type": "vless", "tag": "p", "server": "127.0.0.1", "server_port": port,
			"uuid": u.UUID, "flow": "xtls-rprx-vision",
			"tls": map[string]any{
				"enabled": true, "server_name": m.SNI,
				"utls":    map[string]any{"enabled": true, "fingerprint": "chrome"},
				"reality": map[string]any{"enabled": true, "public_key": m.Pub, "short_id": m.SID},
			},
		}},
	}
	ip, err := runTemporaryClient(cfg, socksPort, port)
	res.ExitIP = ip
	if err != nil {
		res.Note = err.Error()
		return res
	}
	res.OK = ip != "" && (res.Want == "" || ip == res.Want)
	if !res.OK {
		res.Note = "出口 IP 与记录不一致（NAT？）"
	}
	return res
}

func udpSelfTest() SelfTestResult {
	res := SelfTestResult{Scheme: "udp"}
	if !state.UdInstalled() {
		res.OK, res.Note = true, "未安装"
		return res
	}
	m, err := state.LoadUdMeta()
	if err != nil {
		res.Note = "读 meta 失败: " + err.Error()
		return res
	}
	us, _ := state.LoadUdUsers()
	if len(us) == 0 {
		res.Note = "没有用户"
		return res
	}
	u := us[0]
	res.Port = u.Port
	res.Want = m.IP

	socksPort, err := freePort()
	if err != nil {
		res.Note = err.Error()
		return res
	}
	out := map[string]any{
		"type": "hysteria2", "tag": "p", "server": "127.0.0.1", "server_port": u.Port,
		"password": u.Password,
		"tls":      map[string]any{"enabled": true, "insecure": true, "server_name": m.SNI},
	}
	if u.Obfs != "" {
		out["obfs"] = map[string]any{"type": "salamander", "password": u.Obfs}
	}
	cfg := map[string]any{
		"log":       map[string]any{"level": "error"},
		"inbounds":  []any{map[string]any{"type": "socks", "tag": "in", "listen": "127.0.0.1", "listen_port": socksPort}},
		"outbounds": []any{out},
	}
	ip, err := runTemporaryClient(cfg, socksPort, u.Port)
	res.ExitIP = ip
	if err != nil {
		res.Note = err.Error()
		return res
	}
	res.OK = ip != "" && (res.Want == "" || ip == res.Want)
	if !res.OK {
		res.Note = "出口 IP 与记录不一致（NAT？）"
	}
	return res
}

// runTemporaryClient 写临时配置、起 sing-box、经本地 SOCKS 请求 ip.sb，最后清理。
func runTemporaryClient(cfg map[string]any, socksPort, targetPort int) (string, error) {
	dir, err := os.MkdirTemp("", "rxctl-selftest-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	conf := dir + "/client.json"
	b, _ := json.MarshalIndent(cfg, "", "  ")
	if err := os.WriteFile(conf, b, 0o600); err != nil {
		return "", err
	}
	cmd := exec.Command("sing-box", "run", "-c", conf)
	cmd.Stdout, cmd.Stderr = nil, nil
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("起临时客户端失败: %w", err)
	}
	defer func() {
		if cmd.Process != nil {
			cmd.Process.Kill()
			cmd.Wait()
		}
	}()

	// 等 SOCKS 端口就绪（最多 6 秒）
	ready := false
	for i := 0; i < 30; i++ {
		c, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(socksPort), 300*time.Millisecond)
		if err == nil {
			c.Close()
			ready = true
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if !ready {
		return "", fmt.Errorf("临时客户端未就绪（端口 %d/%d 是否在听？）", socksPort, targetPort)
	}

	out := check.Run("curl", "-s", "--max-time", "25", "--socks5-hostname",
		fmt.Sprintf("127.0.0.1:%d", socksPort), "https://ip.sb")
	if out == "" {
		return "", fmt.Errorf("经临时客户端出网失败（看 journalctl -u %s）", unitName(selftestUnit(cfg)))
	}
	return firstLine(out), nil
}

func selftestUnit(cfg map[string]any) string {
	obs, _ := cfg["outbounds"].([]any)
	if len(obs) > 0 {
		if m, ok := obs[0].(map[string]any); ok {
			if t, _ := m["type"].(string); t == "hysteria2" {
				return "udp"
			}
		}
	}
	return "tcp"
}

func firstLine(s string) string {
	for i, r := range s {
		if r == '\n' || r == '\r' {
			return s[:i]
		}
	}
	return s
}

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}
