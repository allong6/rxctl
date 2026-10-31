package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/allong6/rxctl/internal/links"
	"github.com/allong6/rxctl/internal/state"
)

// exportNode 是导出时统一后的一个节点。
type exportNode struct {
	Name     string
	Scheme   string // tcp | udp
	Server   string
	Port     int
	UUID     string // tcp
	Password string // udp
	Obfs     string // udp
	SNI      string
	Pub      string // reality public key
	SID      string // reality short id
}

func (n exportNode) tag() string {
	if n.Scheme == "tcp" {
		return n.Name + "-tcp"
	}
	return n.Name + "-udp"
}

func (n exportNode) uri() string {
	if n.Scheme == "tcp" {
		m := state.TcpMeta{PubIP: n.Server, Port: n.Port, SNI: n.SNI, Pub: n.Pub, SID: n.SID}
		u := state.TcpUser{Name: n.Name, UUID: n.UUID, Port: n.Port}
		return links.Vless(m, u)
	}
	m := state.UdMeta{IP: n.Server, SNI: n.SNI}
	u := state.UdUser{Name: n.Name, Password: n.Password, Port: n.Port, Obfs: n.Obfs}
	return links.Hysteria2(n.Server, m, u)
}

func cmdExport(rest []string) int {
	fs := flag.NewFlagSet("export", flag.ContinueOnError)
	format := fs.String("format", "uri", "输出格式：uri | clash | singbox")
	user := fs.String("user", "", "只导出某个用户（默认全部）")
	scheme := fs.String("scheme", "all", "tcp | udp | all")
	server := fs.String("server", "", "覆盖服务器地址（如经 A 中转时填 120.26.2.197）")
	port := fs.Int("port", 0, "覆盖端口（仅单节点时可用）")
	portMap := fs.String("port-map", "", "端口映射，如 24443=28544,24444=28545（经 A 中转时用）")
	rules := fs.String("rules", "cn", "是否内置分流：cn | none")
	root := fs.String("root", "", "配置根目录（测试用）")
	fs.SetOutput(os.Stderr)
	if err := fs.Parse(rest); err != nil {
		return 2
	}
	if *root != "" {
		state.Root = *root
	}

	f := strings.ToLower(*format)
	switch f {
	case "mihomo":
		f = "clash"
	case "sing-box", "sing_box", "sb":
		f = "singbox"
	case "uri", "links":
		f = "uri"
	case "clash", "singbox":
	default:
		return fail2("--format 只能是 uri | clash | singbox")
	}
	sc := normScheme(*scheme)
	if sc != "tcp" && sc != "udp" && sc != "all" {
		return fail2("--scheme 只能是 tcp | udp | all")
	}
	useRules := *rules != "none"

	pm, err := parsePortMap(*portMap)
	if err != nil {
		return fail2(err.Error())
	}
	nodes, err := gatherExportNodes(*user, sc, *server, *port, pm)
	if err != nil {
		return fail(err.Error())
	}

	switch f {
	case "uri":
		for _, n := range nodes {
			fmt.Println(n.uri())
		}
	case "clash":
		fmt.Print(renderClash(nodes, useRules))
	case "singbox":
		b, err := renderSingbox(nodes, useRules)
		if err != nil {
			return fail(err.Error())
		}
		fmt.Print(b)
	}
	return 0
}

// parsePortMap 解析 "24443=28544,24444=28545"。
func parsePortMap(s string) (map[int]int, error) {
	out := map[int]int{}
	if strings.TrimSpace(s) == "" {
		return out, nil
	}
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		k, v, ok := strings.Cut(part, "=")
		if !ok {
			return nil, fmt.Errorf("--port-map 格式应为 from=to: %q", part)
		}
		from, err1 := strconv.Atoi(strings.TrimSpace(k))
		to, err2 := strconv.Atoi(strings.TrimSpace(v))
		if err1 != nil || err2 != nil || from <= 0 || to <= 0 || from > 65535 || to > 65535 {
			return nil, fmt.Errorf("--port-map 端口不合法: %q", part)
		}
		out[from] = to
	}
	return out, nil
}

// gatherExportNodes 从状态文件收集节点，并应用覆盖参数。
func gatherExportNodes(user, scheme, server string, port int, pm map[int]int) ([]exportNode, error) {
	var nodes []exportNode

	if scheme == "tcp" || scheme == "all" {
		if state.TcpInstalled() {
			m, err := state.LoadTcpMeta()
			if err == nil {
				us, _ := state.LoadTcpUsers()
				for _, u := range us {
					if user != "" && u.Name != user {
						continue
					}
					p := u.Port
					if p == 0 {
						p = m.Port
					}
					if np, ok := pm[p]; ok {
						p = np
					}
					srv := m.PubIP
					if server != "" {
						srv = server
					}
					nodes = append(nodes, exportNode{
						Name: u.Name, Scheme: "tcp", Server: srv, Port: p,
						UUID: u.UUID, SNI: m.SNI, Pub: m.Pub, SID: m.SID,
					})
				}
			}
		}
	}
	if scheme == "udp" || scheme == "all" {
		if state.UdInstalled() {
			m, err := state.LoadUdMeta()
			if err == nil {
				ip := m.IP
				if ip == "" {
					if tm, e := state.LoadTcpMeta(); e == nil {
						ip = tm.PubIP
					}
				}
				us, _ := state.LoadUdUsers()
				for _, u := range us {
					if user != "" && u.Name != user {
						continue
					}
					p := u.Port
					if p == 0 {
						p = m.Port
					}
					if np, ok := pm[p]; ok {
						p = np
					}
					srv := ip
					if server != "" {
						srv = server
					}
					nodes = append(nodes, exportNode{
						Name: u.Name, Scheme: "udp", Server: srv, Port: p,
						Password: u.Password, Obfs: u.Obfs, SNI: m.SNI,
					})
				}
			}
		}
	}
	if len(nodes) == 0 {
		return nil, fmt.Errorf("没有可导出的节点（未安装 / 用户不存在 / scheme 不匹配）")
	}
	if port > 0 {
		if len(nodes) != 1 {
			return nil, fmt.Errorf("--port 只能用于单个节点；多个节点请用 --port-map")
		}
		nodes[0].Port = port
	}
	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].Name != nodes[j].Name {
			return nodes[i].Name < nodes[j].Name
		}
		return nodes[i].Scheme < nodes[j].Scheme
	})
	return nodes, nil
}

func yq(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// renderClash 生成完整的 Clash/Mihomo profile。
func renderClash(nodes []exportNode, rules bool) string {
	var b strings.Builder
	b.WriteString("mixed-port: 7890\nallow-lan: false\nmode: rule\nlog-level: info\nipv6: false\n\n")

	if rules {
		b.WriteString("dns:\n")
		b.WriteString("  enable: true\n  ipv6: false\n")
		b.WriteString("  enhanced-mode: fake-ip\n  fake-ip-range: 198.18.0.1/16\n")
		b.WriteString("  nameserver:\n    - \"https://1.1.1.1/dns-query#PROXY\"\n")
		b.WriteString("  nameserver-policy:\n    \"geosite:cn\":\n      - 223.5.5.5\n      - 119.29.29.29\n\n")
	}

	b.WriteString("proxies:\n")
	for _, n := range nodes {
		if n.Scheme == "tcp" {
			fmt.Fprintf(&b, "  - name: %s\n", yq(n.tag()))
			b.WriteString("    type: vless\n")
			fmt.Fprintf(&b, "    server: %s\n    port: %d\n", yq(n.Server), n.Port)
			fmt.Fprintf(&b, "    uuid: %s\n", yq(n.UUID))
			b.WriteString("    network: tcp\n    udp: true\n    tls: true\n")
			b.WriteString("    flow: xtls-rprx-vision\n")
			fmt.Fprintf(&b, "    servername: %s\n", yq(n.SNI))
			b.WriteString("    client-fingerprint: chrome\n")
			b.WriteString("    reality-opts:\n")
			fmt.Fprintf(&b, "      public-key: %s\n      short-id: %s\n", yq(n.Pub), yq(n.SID))
		} else {
			fmt.Fprintf(&b, "  - name: %s\n", yq(n.tag()))
			b.WriteString("    type: hysteria2\n")
			fmt.Fprintf(&b, "    server: %s\n    port: %d\n", yq(n.Server), n.Port)
			fmt.Fprintf(&b, "    password: %s\n", yq(n.Password))
			fmt.Fprintf(&b, "    sni: %s\n", yq(n.SNI))
			b.WriteString("    skip-cert-verify: true\n")
			if n.Obfs != "" {
				b.WriteString("    obfs: salamander\n")
				fmt.Fprintf(&b, "    obfs-password: %s\n", yq(n.Obfs))
			}
		}
	}

	var names []string
	for _, n := range nodes {
		names = append(names, yq(n.tag()))
	}
	b.WriteString("\nproxy-groups:\n")
	b.WriteString("  - name: PROXY\n    type: select\n    proxies:\n")
	for _, s := range names {
		fmt.Fprintf(&b, "      - %s\n", s)
	}
	if len(nodes) > 1 {
		b.WriteString("  - name: AUTO\n    type: url-test\n")
		b.WriteString("    url: \"http://www.gstatic.com/generate_204\"\n    interval: 300\n    proxies:\n")
		for _, s := range names {
			fmt.Fprintf(&b, "      - %s\n", s)
		}
	}

	b.WriteString("\nrules:\n")
	if rules {
		b.WriteString("  - GEOSITE,cn,DIRECT\n  - GEOIP,CN,DIRECT\n")
	}
	b.WriteString("  - MATCH,PROXY\n")
	return b.String()
}

// renderSingbox 生成完整的 sing-box 客户端配置。
func renderSingbox(nodes []exportNode, rules bool) (string, error) {
	var outbounds []any
	var names []string
	for _, n := range nodes {
		var ob map[string]any
		if n.Scheme == "tcp" {
			ob = map[string]any{
				"type": "vless", "tag": n.tag(), "server": n.Server, "server_port": n.Port,
				"uuid": n.UUID, "flow": "xtls-rprx-vision",
				"tls": map[string]any{
					"enabled": true, "server_name": n.SNI,
					"utls":    map[string]any{"enabled": true, "fingerprint": "chrome"},
					"reality": map[string]any{"enabled": true, "public_key": n.Pub, "short_id": n.SID},
				},
			}
		} else {
			tls := map[string]any{"enabled": true, "server_name": n.SNI, "insecure": true}
			ob = map[string]any{
				"type": "hysteria2", "tag": n.tag(), "server": n.Server, "server_port": n.Port,
				"password": n.Password, "tls": tls,
			}
			if n.Obfs != "" {
				ob["obfs"] = map[string]any{"type": "salamander", "password": n.Obfs}
			}
		}
		outbounds = append(outbounds, ob)
		names = append(names, n.tag())
	}
	outbounds = append(outbounds, map[string]any{
		"type": "selector", "tag": "PROXY", "outbounds": names, "default": names[0],
	})
	outbounds = append(outbounds, map[string]any{"type": "direct", "tag": "direct"})

	route := map[string]any{"final": "PROXY", "auto_detect_interface": true}
	dns := map[string]any{
		"servers": []any{
			map[string]any{"tag": "local", "type": "udp", "server": "223.5.5.5"},
			map[string]any{"tag": "remote", "type": "https", "server": "1.1.1.1", "detour": "PROXY"},
		},
		"final": "remote",
	}
	if rules {
		route["rule_set"] = []any{
			map[string]any{
				"tag": "geosite-cn", "type": "remote", "format": "binary",
				"url":             "https://raw.githubusercontent.com/SagerNet/sing-geosite/rule-set/geosite-cn.srs",
				"download_detour": "PROXY",
			},
			map[string]any{
				"tag": "geoip-cn", "type": "remote", "format": "binary",
				"url":             "https://raw.githubusercontent.com/SagerNet/sing-geoip/rule-set/geoip-cn.srs",
				"download_detour": "PROXY",
			},
		}
		route["rules"] = []any{
			map[string]any{"action": "sniff"},
			map[string]any{"ip_is_private": true, "outbound": "direct"},
			map[string]any{"rule_set": []string{"geosite-cn"}, "outbound": "direct"},
			map[string]any{"rule_set": []string{"geoip-cn"}, "outbound": "direct"},
		}
		dns["rules"] = []any{
			map[string]any{"domain_suffix": []string{".cn", "baidu.com", "qq.com", "taobao.com", "jd.com", "bilibili.com", "aliyun.com", "163.com", "weibo.com", "zhihu.com"}, "server": "local"},
		}
	}

	cfg := map[string]any{
		"log":       map[string]any{"level": "info", "timestamp": true},
		"dns":       dns,
		"inbounds":  []any{map[string]any{"type": "mixed", "tag": "mixed-in", "listen": "127.0.0.1", "listen_port": 7890}},
		"outbounds": outbounds,
		"route":     route,
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b) + "\n", nil
}
