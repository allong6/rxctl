// Package links 生成客户端可导入的分享链接。
package links

import (
	"fmt"
	"net/url"

	"rxctl/internal/state"
)

// Vless 生成 vless:// 链接。
func Vless(m state.TcpMeta, u state.TcpUser) string {
	port := u.Port
	if port == 0 {
		port = m.Port
	}
	q := url.Values{}
	q.Set("encryption", "none")
	q.Set("flow", "xtls-rprx-vision")
	q.Set("security", "reality")
	q.Set("sni", m.SNI)
	q.Set("fp", "chrome")
	q.Set("pbk", m.Pub)
	q.Set("sid", m.SID)
	q.Set("type", "tcp")
	return fmt.Sprintf("vless://%s@%s:%d?%s#%s", u.UUID, m.PubIP, port, q.Encode(), url.PathEscape(u.Name))
}

// Hysteria2 生成 hysteria2:// 链接。
func Hysteria2(ip string, m state.UdMeta, u state.UdUser) string {
	q := url.Values{}
	q.Set("insecure", "1")
	if m.SNI != "" {
		q.Set("sni", m.SNI)
	}
	if u.Obfs != "" {
		q.Set("obfs", "salamander")
		q.Set("obfs-password", u.Obfs)
	}
	return fmt.Sprintf("hysteria2://%s@%s:%d/?%s#hy2-%s",
		url.PathEscape(u.Password), ip, u.Port, q.Encode(), url.PathEscape(u.Name))
}
