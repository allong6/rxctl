package engine

import (
	"encoding/json"
	"os"
	"sort"

	"github.com/allong6/rxctl/internal/state"
)

// tcpInbound 按端口分组建 TCP 入站。
func tcpInbounds(m state.TcpMeta, users []state.TcpUser) []any {
	byPort := map[int][]state.TcpUser{}
	for _, u := range users {
		p := u.Port
		if p == 0 {
			p = m.Port
		}
		byPort[p] = append(byPort[p], u)
	}
	var ports []int
	for p := range byPort {
		ports = append(ports, p)
	}
	sort.Ints(ports)
	out := make([]any, 0, len(ports))
	for _, p := range ports {
		var us []any
		for _, u := range byPort[p] {
			us = append(us, map[string]any{"name": u.Name, "uuid": u.UUID, "flow": "xtls-rprx-vision"})
		}
		out = append(out, map[string]any{
			"type": "vless", "tag": "in_" + itoa(p), "listen": "0.0.0.0", "listen_port": p,
			"users": us,
			"tls": map[string]any{
				"enabled": true, "server_name": m.SNI,
				"reality": map[string]any{
					"enabled":     true,
					"handshake":   map[string]any{"server": m.SNI, "server_port": 443},
					"private_key": m.Priv,
					"short_id":    []string{m.SID},
				},
			},
		})
	}
	return out
}

// tcpConfig 生成 TCP 方案完整配置。
func tcpConfig(m state.TcpMeta, users []state.TcpUser) map[string]any {
	return map[string]any{
		"log":       map[string]any{"level": "info", "timestamp": true},
		"inbounds":  tcpInbounds(m, users),
		"outbounds": []any{map[string]any{"type": "direct", "tag": "direct"}},
	}
}

// udpConfig 生成 UDP 方案完整配置：每个用户一个 hysteria2 入站。
func udpConfig(m state.UdMeta, users []state.UdUser) map[string]any {
	ins := make([]any, 0, len(users))
	for _, u := range users {
		in := map[string]any{
			"type": "hysteria2", "tag": "hy_" + u.Name, "listen": "0.0.0.0", "listen_port": u.Port,
			"users": []any{map[string]any{"password": u.Password}},
			"tls": map[string]any{
				"enabled": true, "certificate_path": m.Cert, "key_path": m.Key,
			},
		}
		if u.Obfs != "" {
			in["obfs"] = map[string]any{"type": "salamander", "password": u.Obfs}
		}
		ins = append(ins, in)
	}
	return map[string]any{
		"log":       map[string]any{"level": "info", "timestamp": true},
		"inbounds":  ins,
		"outbounds": []any{map[string]any{"type": "direct", "tag": "direct"}},
	}
}

// writeJSON 原子写 JSON（600）。
func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return writeFileAtomic(path, b, 0o600)
}

// writeFileAtomic 临时文件 + rename。
func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	f.Close()
	return os.Rename(tmp, path)
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}

func quotaWord(q int64) string {
	if q > 0 {
		return state.Human(q)
	}
	return "none"
}
