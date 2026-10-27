package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"rxctl/internal/check"
	"rxctl/internal/engine"
	"rxctl/internal/state"
	"rxctl/internal/ui"
)

// agentState 是 agent 的本地状态（也是 M2 的"上报"落点：先落盘，M3 再接配置中心）。
type agentState struct {
	Version    string                 `json:"version"`
	StartedAt  string                 `json:"started_at"`
	LastTick   string                 `json:"last_tick"`
	Ticks      int64                  `json:"ticks"`
	LastError  string                 `json:"last_error,omitempty"`
	LastMS     int64                  `json:"last_duration_ms"`
	Interval   string                 `json:"interval"`
	Restarts   map[string]int         `json:"restarts"`
	LastHeal   map[string]string      `json:"last_heal,omitempty"`
	Schemes    map[string]agentScheme `json:"schemes"`
	LastRows   []engine.Row           `json:"last_rows,omitempty"`
	LastRunOut string                 `json:"last_run_at"`
}

type agentScheme struct {
	Installed bool  `json:"installed"`
	Active    bool  `json:"active"`
	Ports     []int `json:"ports"`
	Users     int   `json:"users"`
}

func agentStateDir() string { return state.Path("/var/lib/rxctl") }
func agentStateFile() string {
	return filepath.Join(agentStateDir(), "agent.json")
}

func loadAgentState() agentState {
	st := agentState{Restarts: map[string]int{}, LastHeal: map[string]string{}, Schemes: map[string]agentScheme{}}
	b, err := os.ReadFile(agentStateFile())
	if err != nil {
		return st
	}
	_ = json.Unmarshal(b, &st)
	if st.Restarts == nil {
		st.Restarts = map[string]int{}
	}
	if st.LastHeal == nil {
		st.LastHeal = map[string]string{}
	}
	if st.Schemes == nil {
		st.Schemes = map[string]agentScheme{}
	}
	return st
}

func saveAgentState(st agentState) error {
	if err := os.MkdirAll(agentStateDir(), 0o700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(st, "", "  ")
	b = append(b, '\n')
	tmp := agentStateFile() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, agentStateFile())
}

func unitAgentPath() string {
	return state.Path("/etc/systemd/system/rxctl-agent.service")
}

// cmdAgent 是 M2 的常驻进程：定时累计用量 + 服务自愈 + 状态落盘。
func cmdAgent(rest []string) int {
	sub := "run"
	if len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
		sub = rest[0]
		rest = rest[1:]
	}
	switch sub {
	case "run", "start":
		return agentRun(rest, false)
	case "once", "tick":
		return agentRun(rest, true)
	case "install", "enable":
		return agentInstall(rest)
	case "uninstall", "disable":
		return agentUninstall()
	case "status":
		return agentStatus(rest)
	}
	return fail2("用法: rxctl agent <run|once|install|uninstall|status> [--interval 10m] [--json]")
}

func agentFlags(rest []string) (interval time.Duration, jsonOut bool, err error) {
	fs := flag.NewFlagSet("agent", flag.ContinueOnError)
	iv := fs.String("interval", "10m", "统计间隔（如 5m / 30s）")
	j := fs.Bool("json", false, "机器可读")
	fs.SetOutput(os.Stderr)
	if err := fs.Parse(rest); err != nil {
		return 0, false, err
	}
	d, perr := time.ParseDuration(*iv)
	if perr != nil || d < 30*time.Second {
		return 0, false, fmt.Errorf("--interval 不合法（最小 30s）: %s", *iv)
	}
	return d, *j, nil
}

func agentRun(rest []string, once bool) int {
	interval, _, err := agentFlags(rest)
	if err != nil {
		return fail2(err.Error())
	}
	if os.Geteuid() != 0 && state.Root == "" {
		return fail("需要 root（iptables / systemctl）")
	}
	log.SetPrefix("rxctl-agent: ")
	log.SetFlags(log.LstdFlags)

	st := loadAgentState()
	st.Version = version
	st.Interval = interval.String()
	if st.StartedAt == "" {
		st.StartedAt = time.Now().Format(time.RFC3339)
	}
	log.Printf("start interval=%s once=%v", interval, once)

	for {
		agentTick(&st)
		if once {
			if st.LastError != "" {
				return 5
			}
			return 0
		}
		time.Sleep(interval)
	}
}

// agentTick 跑一轮：累计用量 → 检查服务 → 必要时重启 → 落盘。
func agentTick(st *agentState) {
	start := time.Now()
	st.Ticks++
	st.LastTick = start.Format(time.RFC3339)

	rows, err := engine.QuotaCheck()
	if err != nil {
		st.LastError = err.Error()
		log.Printf("quota check failed: %v", err)
	} else {
		st.LastError = ""
		st.LastRows = rows
	}

	st.Schemes = map[string]agentScheme{}
	for _, sc := range []string{"tcp", "udp"} {
		installed := (sc == "tcp" && state.TcpInstalled()) || (sc == "udp" && state.UdInstalled())
		as := agentScheme{Installed: installed}
		if installed {
			as.Active = engine.SvcActive(sc)
			for _, u := range state.UsersOf(sc) {
				as.Users++
				if u.Port > 0 {
					as.Ports = append(as.Ports, u.Port)
				}
			}
			// 自愈：服务掉了就拉起来（同一服务 60 秒内不重复重启，避免风暴）
			if !as.Active {
				last := st.LastHeal[sc]
				canHeal := true
				if last != "" {
					if t, e := time.Parse(time.RFC3339, last); e == nil && time.Since(t) < 60*time.Second {
						canHeal = false
					}
				}
				if canHeal {
					log.Printf("service %s inactive -> restart", sc)
					if e := engine.Svc("restart", sc); e != nil {
						log.Printf("restart %s failed: %v", sc, e)
						st.LastError = "restart " + sc + ": " + e.Error()
					} else {
						st.Restarts[sc]++
						st.LastHeal[sc] = time.Now().Format(time.RFC3339)
						as.Active = engine.SvcActive(sc)
					}
				}
			}
		}
		st.Schemes[sc] = as
	}

	st.LastMS = time.Since(start).Milliseconds()
	st.LastRunOut = time.Now().Format(time.RFC3339)
	if err := saveAgentState(*st); err != nil {
		log.Printf("save state failed: %v", err)
	}
	log.Printf("tick #%d ok=%v duration=%dms", st.Ticks, st.LastError == "", st.LastMS)
}

func agentInstall(rest []string) int {
	interval, _, err := agentFlags(rest)
	if err != nil {
		return fail2(err.Error())
	}
	if os.Geteuid() != 0 && state.Root == "" {
		return fail("需要 root")
	}
	exe, err := os.Executable()
	if err != nil {
		return fail(err.Error())
	}
	unit := `[Unit]
Description=rxctl agent (quota + self-heal)
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=` + exe + ` agent run --interval ` + interval.String() + `
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
`
	if err := os.MkdirAll(filepath.Dir(unitAgentPath()), 0o755); err != nil {
		return fail(err.Error())
	}
	if err := os.WriteFile(unitAgentPath(), []byte(unit), 0o644); err != nil {
		return fail(err.Error())
	}
	check.Run("systemctl", "daemon-reload")
	if out := check.Run("systemctl", "enable", "--now", "rxctl-agent"); out != "" {
		fmt.Println(out)
	}
	// agent 接管后必须去掉 cron，否则两个进程会争同一个锁
	if _, err := os.Stat(state.Path("/etc/cron.d/rxctl-quota")); err == nil {
		_ = os.Remove(state.Path("/etc/cron.d/rxctl-quota"))
		fmt.Println("已移除 /etc/cron.d/rxctl-quota（改由 agent 接管）")
	}
	time.Sleep(500 * time.Millisecond)
	fmt.Println("OK agent 已安装并启动：rxctl-agent.service（interval " + interval.String() + "）")
	fmt.Println("   查看: rxctl agent status")
	return 0
}

func agentUninstall() int {
	if os.Geteuid() != 0 && state.Root == "" {
		return fail("需要 root")
	}
	check.Run("systemctl", "disable", "--now", "rxctl-agent")
	if err := os.Remove(unitAgentPath()); err != nil && !os.IsNotExist(err) {
		return fail(err.Error())
	}
	check.Run("systemctl", "daemon-reload")
	fmt.Println("OK agent 已停止并移除（如需恢复定时统计：rxctl quota cron）")
	return 0
}

func agentStatus(rest []string) int {
	fs := flag.NewFlagSet("agent status", flag.ContinueOnError)
	j := fs.Bool("json", false, "机器可读")
	fs.SetOutput(os.Stderr)
	if err := fs.Parse(rest); err != nil {
		return 2
	}
	st := loadAgentState()
	running := check.ServiceActive("rxctl-agent")
	if *j {
		ui.JSON(os.Stdout, map[string]any{
			"service_active": running,
			"state":          st,
			"state_file":     agentStateFile(),
		})
		return 0
	}
	fmt.Printf("服务      : %s (%s)\n", onOff(running), "rxctl-agent.service")
	fmt.Printf("状态文件  : %s\n", nz(agentStateFile(), "-"))
	fmt.Printf("启动时间  : %s\n", nz(st.StartedAt, "-"))
	fmt.Printf("上次执行  : %s（第 %d 次，耗时 %d ms）\n", nz(st.LastTick, "-"), st.Ticks, st.LastMS)
	if st.LastError != "" {
		fmt.Printf("上次错误  : %s\n", st.LastError)
	}
	if len(st.LastRows) > 0 {
		var out [][]string
		for _, r := range st.LastRows {
			q := "none"
			if r.Quota > 0 {
				q = state.Human(r.Quota)
			}
			out = append(out, []string{r.Name, r.Scheme, fmt.Sprint(r.Port), state.Human(r.Used), q, r.Percent, r.Status})
		}
		ui.Table(os.Stdout, []string{"用户", "方案", "端口", "已用", "配额", "占比", "状态"}, out)
	}
	for _, sc := range []string{"tcp", "udp"} {
		as, ok := st.Schemes[sc]
		if !ok || !as.Installed {
			continue
		}
		fmt.Printf("%-9s : %s · 端口 %v · 用户 %d · 自动重启 %d 次\n", sc, onOff(as.Active), as.Ports, as.Users, st.Restarts[sc])
	}
	return 0
}

func onOff(b bool) string {
	if b {
		return "active"
	}
	return "inactive"
}

// agentLastTick 给 doctor 用：返回"上次执行时间"的可读片段。
func agentLastTick() string {
	st := loadAgentState()
	if st.LastTick == "" {
		return ""
	}
	return "上次 " + st.LastTick
}
