package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/allong6/rxctl/internal/state"
)

func toI64(s string) int64 { return parseInt64(s) }
func atoi(s string) int    { return parseInt(s) }

// Row 是一行用量结果。
type Row struct {
	Name    string `json:"name"`
	Scheme  string `json:"scheme"`
	Port    int    `json:"port"`
	Quota   int64  `json:"quota"`
	Used    int64  `json:"used"`
	In      int64  `json:"in"`
	Percent string `json:"percent"`
	Status  string `json:"status"`
	Month   string `json:"month,omitempty"`
}

func parseInt(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}

func parseInt64(s string) int64 {
	n, _ := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	return n
}

// QuotaCheck 累计用量、超配额封端口、跨月重置。返回当前报表。
// 并发保护：cron 与 agent 同时触发时只有一个能跑，避免重复累计。
func QuotaCheck() ([]Row, error) {
	unlock, err := lockFile(filepath.Join(os.TempDir(), "rxctl-quota.lock"))
	if err != nil {
		return nil, fmt.Errorf("另一个 quota 进程正在运行（锁 %s）", filepath.Join(os.TempDir(), "rxctl-quota.lock"))
	}
	defer unlock()

	month := time.Now().Format("2006-01")
	var rows []Row

	rows = append(rows, checkScheme("tcp", month)...)
	rows = append(rows, checkScheme("udp", month)...)

	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Scheme != rows[j].Scheme {
			return rows[i].Scheme < rows[j].Scheme
		}
		return rows[i].Name < rows[j].Name
	})
	return rows, nil
}

// checkScheme 对一套方案做累计/熔断/重置，并回写 usage.tsv。
func checkScheme(scheme, month string) []Row {
	installed := (scheme == "tcp" && state.TcpInstalled()) || (scheme == "udp" && state.UdInstalled())
	if !installed {
		return nil
	}
	proto := scheme
	FwEnsure()

	prev := map[string]state.Usage{}
	if us, err := state.LoadUsage(scheme); err == nil {
		for _, u := range us {
			prev[u.Name] = u
		}
	}
	outs := Counters(proto, "spt")
	ins := Counters(proto, "dpt")

	var (
		rows []Row
		all  []state.Usage
	)
	for _, u := range state.UsersOf(scheme) {
		if u.Port <= 0 { // 无独立端口：不计量（共享默认端口，内核分不清用户）
			continue
		}
		FwEnsurePort(proto, u.Port)
		r, rec := accumulate(scheme, u.Name, u.Port, u.Quota, outs[u.Port], ins[u.Port], prev[u.Name], month)
		rows = append(rows, r)
		all = append(all, rec)
		if r.Status == "已封" {
			FwBlockPort(proto, u.Port)
		} else if prev[u.Name].Blocked {
			FwUnblock(proto, u.Port)
		}
	}
	if err := writeUsage(scheme, all); err != nil {
		// 写不进去不影响本次输出，但会体现在下次累计上
	}
	return rows
}

// accumulate 纯函数：按"上次计数→本次计数"的增量累加，跨月清零。
func accumulate(scheme, name string, port int, quota, curOut, curIn int64, prev state.Usage, month string) (Row, state.Usage) {
	used, in := int64(0), int64(0)
	blocked := false
	if prev.Month == month {
		used, in = prev.Out, prev.In
		if curOut >= prev.LastOut {
			used += curOut - prev.LastOut
		} else {
			// 内核计数被清零（重启/规则重建）→ 把当前计数当作新一段的增量
			used += curOut
		}
		if curIn >= prev.LastIn {
			in += curIn - prev.LastIn
		} else {
			in += curIn
		}
	} else {
		// 跨月：计数从 0 起算（当月第一次累计 = 当前计数）
		used = curOut
		in = curIn
	}
	if quota > 0 && used >= quota {
		blocked = true
	}
	pct := "-"
	status := "正常"
	if quota > 0 {
		pct = fmt.Sprintf("%.1f%%", float64(used)*100/float64(quota))
		if blocked {
			status = "已封"
		}
	}
	return Row{Name: name, Scheme: scheme, Port: port, Quota: quota, Used: used, In: in, Percent: pct, Status: status, Month: month},
		state.Usage{Name: name, Port: port, Quota: quota, Month: month, Out: used, In: in, LastOut: curOut, LastIn: curIn, Blocked: blocked}
}

func writeUsage(scheme string, all []state.Usage) error {
	var b strings.Builder
	for _, u := range all {
		blk := 0
		if u.Blocked {
			blk = 1
		}
		fmt.Fprintf(&b, "%s\t%d\t%d\t%s\t%d\t%d\t%d\t%d\t%d\n",
			u.Name, u.Port, u.Quota, u.Month, u.Out, u.In, u.LastOut, u.LastIn, blk)
	}
	return state.WriteUsage(scheme, []byte(b.String()))
}

// UsageRows 只读地汇总用量（不累计、不改状态）。
func UsageRows(month string) ([]Row, error) {
	var rows []Row
	for _, scheme := range []string{"tcp", "udp"} {
		us, err := state.LoadUsage(scheme)
		if err != nil {
			continue
		}
		for _, u := range us {
			if month != "" && u.Month != month {
				continue
			}
			pct := "-"
			if u.Quota > 0 {
				pct = fmt.Sprintf("%.1f%%", float64(u.Out)*100/float64(u.Quota))
			}
			st := "正常"
			if u.Blocked {
				st = "已封"
			}
			rows = append(rows, Row{Name: u.Name, Scheme: scheme, Port: u.Port, Quota: u.Quota,
				Used: u.Out, In: u.In, Percent: pct, Status: st, Month: u.Month})
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Scheme != rows[j].Scheme {
			return rows[i].Scheme < rows[j].Scheme
		}
		return rows[i].Name < rows[j].Name
	})
	return rows, nil
}
