package engine

import (
	"regexp"
	"strings"

	"rxctl/internal/check"
)

const (
	ChainOut = "RX_USAGE"
	ChainIn  = "RX_USAGE_IN"
)

func ipt(args ...string) string { return check.Run("iptables", args...) }

// iptOK 执行 iptables 并返回是否成功（-C/-D 成功时没有输出，必须看退出码）。
func iptOK(args ...string) bool { return check.OK("iptables", args...) }

// FwEnsure 建立计数链与 jump（幂等，不重置已有计数）。
func FwEnsure() {
	ipt("-N", ChainOut)
	ipt("-N", ChainIn)

	ensureSingleJump("OUTPUT", ChainOut)
	ensureSingleJump("INPUT", ChainIn)

	out := ipt("-S", ChainOut)
	if out != "" && !strings.Contains(out, "-A "+ChainOut+" -j RETURN") {
		ipt("-A", ChainOut, "-j", "RETURN")
	}
	in := ipt("-S", ChainIn)
	if in != "" && !strings.Contains(in, "-A "+ChainIn+" -j RETURN") {
		ipt("-A", ChainIn, "-j", "RETURN")
	}
}

// countJump 数某链里指向 target 的 jump 条数。
func countJump(chain, target string) int {
	n := 0
	for _, l := range strings.Split(ipt("-S", chain), "\n") {
		if strings.TrimSpace(l) == "-A "+chain+" -j "+target {
			n++
		}
	}
	return n
}

// ensureSingleJump 保证 jump 恰好一条：多于一条删到一条，没有才插入。
// 不整条重建，避免每次调用都短暂断流。
func ensureSingleJump(chain, target string) {
	n := countJump(chain, target)
	for n > 1 {
		ipt("-D", chain, "-j", target)
		n--
	}
	if n == 0 {
		ipt("-I", chain, "1", "-j", target)
	}
}

// FwEnsurePort 只在缺规则时补一条，不重建 → 不清零内核计数。
// 用于每次 quota check / 开机恢复。
func FwEnsurePort(proto string, port int) {
	if port <= 0 {
		return
	}
	p := itoa(port)
	if !iptOK("-C", ChainOut, "-p", proto, "--sport", p, "-j", "RETURN") {
		ipt("-I", ChainOut, "1", "-p", proto, "--sport", p, "-j", "RETURN")
	}
	if !iptOK("-C", ChainIn, "-p", proto, "--dport", p, "-j", "RETURN") {
		ipt("-I", ChainIn, "1", "-p", proto, "--dport", p, "-j", "RETURN")
	}
}

// FwWatchPort 让某协议+端口参与计数（幂等）。
func FwWatchPort(proto string, port int) {
	if port <= 0 {
		return
	}
	p := itoa(port)
	// 先删同 spec 的旧规则，再插一条：避免重复规则导致重复计数
	for i := 0; i < 8; i++ {
		if !iptOK("-D", ChainOut, "-p", proto, "--sport", p, "-j", "RETURN") {
			break
		}
	}
	for i := 0; i < 8; i++ {
		if !iptOK("-D", ChainIn, "-p", proto, "--dport", p, "-j", "RETURN") {
			break
		}
	}
	ipt("-I", ChainOut, "1", "-p", proto, "--sport", p, "-j", "RETURN")
	ipt("-I", ChainIn, "1", "-p", proto, "--dport", p, "-j", "RETURN")
}

// FwUnwatchPort 停止计数并解封。
func FwUnwatchPort(proto string, port int) {
	if port <= 0 {
		return
	}
	p := itoa(port)
	for i := 0; i < 8; i++ {
		if !iptOK("-D", ChainOut, "-p", proto, "--sport", p, "-j", "RETURN") {
			break
		}
	}
	for i := 0; i < 8; i++ {
		if !iptOK("-D", ChainIn, "-p", proto, "--dport", p, "-j", "RETURN") {
			break
		}
	}
	FwUnblock(proto, port)
}

// FwBlockPort / FwUnblock 封禁与解封（按协议）。
func FwBlockPort(proto string, port int) {
	if port <= 0 {
		return
	}
	p := itoa(port)
	if !iptOK("-C", "INPUT", "-p", proto, "--dport", p, "-j", "DROP") {
		ipt("-I", "INPUT", "1", "-p", proto, "--dport", p, "-j", "DROP")
	}
}

func FwUnblock(proto string, port int) {
	if port <= 0 {
		return
	}
	p := itoa(port)
	for i := 0; i < 8; i++ {
		if !iptOK("-D", "INPUT", "-p", proto, "--dport", p, "-j", "DROP") {
			break
		}
	}
}

// FwOpen 本机防火墙放行（firewalld / ufw）。
func FwOpen(proto string, port int) string {
	p := itoa(port)
	if check.Has("firewall-cmd") {
		check.Run("firewall-cmd", "--permanent", "--add-port="+p+"/"+proto)
		check.Run("firewall-cmd", "--reload")
		return "firewalld: " + p + "/" + proto
	}
	if check.Has("ufw") {
		check.Run("ufw", "allow", p+"/"+proto)
		return "ufw: " + p + "/" + proto
	}
	return "no managed firewall"
}

// Counters 读计数链，返回 端口 -> 字节（只统计指定协议与字段）。
// field: "spt"（出方向，服务器发出＝用户下载）| "dpt"（入方向，用户上传）。
func Counters(proto, field string) map[int]int64 {
	out := map[int]int64{}
	chain := ChainOut
	if field == "dpt" {
		chain = ChainIn
	}
	raw := check.Run("iptables", "-L", chain, "-n", "-v", "-x")
	if raw == "" {
		return out
	}
	re := regexp.MustCompile(`(?m)^\s*(\d+)\s+(\d+)\s+\S+\s+(\S+)\s+.*\b(spt|dpt):(\d+)\s*$`)
	for _, m := range re.FindAllStringSubmatch(raw, -1) {
		if m[3] != proto || m[4] != field {
			continue
		}
		out[atoi(m[5])] = toI64(m[2])
	}
	return out
}
