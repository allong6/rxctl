// rxctl —— 代理出口服务器管理（TCP: VLESS+Reality / UDP: Hysteria2）
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"rxctl/internal/check"
	"rxctl/internal/engine"
	"rxctl/internal/links"
	"rxctl/internal/state"
	"rxctl/internal/ui"
)

// 由 -ldflags 注入
var (
	version = "dev"
	commit  = "none"
	build   = "unknown"
)

const usageText = `rxctl — 代理出口服务器管理

用法:
  rxctl <command> [subcommand] [flags]

常用命令:
  install <tcp|udp|all>    安装方案（tcp = VLESS+Reality，udp = Hysteria2）
  status [tcp|udp|all]     服务、端口、用户数
  user add|list|del|show   用户管理（可分配独立端口与配额）
  links                    列出客户端链接
  usage                    用量与配额
  quota check|cron         配额熔断与定时任务
  agent run|install|status 常驻：定时统计 + 服务自愈（替代 cron）
  selftest [tcp|udp|all]   真实出网自检
  logs [tcp|udp] [-f]      日志
  doctor [--selftest]      体检
  start|stop|restart       服务控制
  port <tcp|udp> <N>       改默认端口
  uninstall <tcp|udp|all>  卸载

支持方案:
  tcp   VLESS + Reality    sing-box vless 入站（免证书）
  udp   Hysteria2 (QUIC)   sing-box hysteria2 入站（自签证书，可选 obfs）
本机安装情况见: rxctl status
`

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		fmt.Print(usageText)
		return 0
	}

	for len(args) > 0 {
		switch {
		case args[0] == "--root" && len(args) > 1:
			state.Root = args[1]
			args = args[2:]
			continue
		case strings.HasPrefix(args[0], "--root="):
			state.Root = strings.TrimPrefix(args[0], "--root=")
			args = args[1:]
			continue
		}
		switch args[0] {
		case "-h", "--help", "help":
			if len(args) > 1 && args[0] == "help" {
				return helpFor(args[1])
			}
			fmt.Print(usageText)
			return 0
		case "-V", "--version":
			fmt.Printf("rxctl %s (%s/%s)\n", version, osName(), archName())
			return 0
		default:
			goto dispatch
		}
	}

dispatch:
	cmd := args[0]
	rest := args[1:]

	// 命令别名
	switch cmd {
	case "log":
		cmd = "logs"
	case "users":
		cmd = "user"
	case "link":
		cmd = "links"
	case "test":
		cmd = "selftest"
	case "hy", "hysteria2":
		cmd = "hy2"
	}

	switch cmd {
	case "version":
		return cmdVersion(rest)
	case "doctor":
		return cmdDoctor(rest)
	case "status":
		return cmdStatus(rest)
	case "links":
		return cmdLinks(rest)
	case "qr":
		return cmdQR(rest)
	case "usage":
		return cmdUsage(rest)
	case "selftest":
		return cmdSelftest(rest)
	case "install":
		return cmdInstall(rest)
	case "uninstall":
		return cmdUninstall(rest)
	case "user":
		return cmdUser(rest)
	case "quota":
		return cmdQuota(rest)
	case "logs":
		return cmdLogs(rest)
	case "start", "stop", "restart":
		return cmdSvcAction(cmd, rest)
	case "port":
		return cmdPort(rest)
	case "fw", "firewall":
		return cmdFw(rest)
	case "hy2":
		return cmdHy2(rest)
	case "agent":
		return cmdAgent(rest)
	case "completion":
		return cmdCompletion(rest)
	case "center":
		fmt.Printf("命令 %q 属于后续里程碑（M3）\n", cmd)
		return 3
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", cmd)
		fmt.Print(usageText)
		return 2
	}
}

func helpFor(cmd string) int {
	switch cmd {
	case "version", "status", "links", "usage", "doctor", "selftest":
		fmt.Printf("用法: rxctl %s [--json]\n", cmd)
	case "install":
		fmt.Println("用法: rxctl install <tcp|udp|all> [--port N] [--udp-port N] [--sni HOST] [--user NAME] [--quota 500G] [--open-firewall] [--dry-run]")
	case "user":
		fmt.Print(`用法: rxctl user <subcommand>
  list    [--scheme tcp|udp|all] [--json]
  add     <name> [--scheme tcp|udp|both] [--port N] [--quota 500G] [--password P] [--obfs P]
  del     <name> [--scheme ...]
  show    <name> [--scheme ...] [--json] [--qr]
  rotate  <name> [--scheme ...]
  enable|disable <name> [--scheme ...]
  port    <name> <N> [--scheme tcp|udp]
`)
	case "quota":
		fmt.Println("用法: rxctl quota <check|set|cron|uncron|status> ...")
	case "agent":
		fmt.Print(`用法: rxctl agent <subcommand>
  run       [--interval 10m]     前台常驻（systemd 用它）
  once      [--interval 10m]     只跑一轮（排障用）
  install   [--interval 10m]     写 rxctl-agent.service 并启动；自动移除 quota cron
  uninstall                      停止并移除单元
  status    [--json]             看上次执行、服务状态、自动重启次数
`)
	case "hy2":
		return cmdHy2(nil)
	default:
		fmt.Print(usageText)
	}
	return 0
}

// ---------- 通用参数 ----------
type common struct {
	json bool
	root string
	verb bool
}

func parseCommon(fs *flag.FlagSet, rest []string) (common, []string, error) {
	c := common{}
	fs.BoolVar(&c.json, "json", false, "机器可读输出")
	fs.StringVar(&c.root, "root", "", "配置根目录（测试用）")
	fs.BoolVar(&c.verb, "v", false, "打印附加信息")
	fs.SetOutput(os.Stderr)
	if err := fs.Parse(rest); err != nil {
		return c, nil, err
	}
	if c.root != "" {
		state.Root = c.root
	}
	return c, fs.Args(), nil
}

// selfPath 返回当前二进制的绝对路径（sudo 的 secure_path 常常不含 /usr/local/bin，
// 所以提示里必须带全路径，否则 `sudo rxctl` 会 command not found）。
func selfPath() string {
	p, err := os.Executable()
	if err != nil {
		return "rxctl"
	}
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return p
}

// guardStateRead 状态目录是 700：非 root 读不到时明确提示，避免误报"未安装"。
func guardStateRead() int {
	if state.PermissionDenied() {
		exe := selfPath()
		fmt.Fprintln(os.Stderr, "需要 root：状态目录 /etc/reality-exit 的权限是 700，当前用户读不到。")
		fmt.Fprintf(os.Stderr, "请改用: sudo %s <命令>    （例如 sudo %s links）\n", exe, exe)
		return 3
	}
	return 0
}

// ---------- version ----------
type versionOut struct {
	Version string            `json:"version"`
	Commit  string            `json:"commit"`
	Build   string            `json:"build"`
	OS      string            `json:"os"`
	Arch    string            `json:"arch"`
	Deps    map[string]string `json:"deps"`
	Schemes []string          `json:"schemes"`
	Cap     map[string]bool   `json:"capabilities"`
}

func cmdVersion(rest []string) int {
	fs := flag.NewFlagSet("version", flag.ContinueOnError)
	c, _, err := parseCommon(fs, rest)
	if err != nil {
		return 2
	}
	sbVer, sbPath := check.SingboxVersion()
	out := versionOut{
		Version: version, Commit: commit, Build: build,
		OS: osName(), Arch: archName(),
		Deps:    map[string]string{"sing_box": sbVer, "sing_box_path": sbPath},
		Schemes: []string{"tcp", "udp"},
		Cap: map[string]bool{
			"per_user_port": true, "metering": true, "quota_block": true,
			"monthly_reset": true, "qr": true, "tuic": false, "vless_quic": false,
		},
	}
	if c.json {
		ui.JSON(os.Stdout, out)
		return 0
	}
	fmt.Printf("rxctl %s\n", out.Version)
	fmt.Printf("  commit  : %s   build : %s\n", out.Commit, out.Build)
	fmt.Printf("  os/arch : %s/%s\n", out.OS, out.Arch)
	if sbVer == "" {
		fmt.Println("  sing-box: 未找到")
	} else {
		fmt.Printf("  sing-box: %s  (%s)\n", sbVer, sbPath)
	}
	return 0
}

// ---------- status ----------
type schemeStatus struct {
	Installed bool   `json:"installed"`
	Service   string `json:"service"`
	Ports     []int  `json:"ports"`
	Users     int    `json:"users"`
}

func cmdStatus(rest []string) int {
	if code := guardStateRead(); code != 0 {
		return code
	}
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	c, args, err := parseCommon(fs, rest)
	if err != nil {
		return 2
	}
	want := "all"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		want = normScheme(args[0])
	}
	res := map[string]schemeStatus{}
	var rows [][]string

	if want == "all" || want == "tcp" {
		if state.TcpInstalled() {
			m, _ := state.LoadTcpMeta()
			st := schemeStatus{Installed: true, Service: svc("reality-exit")}
			us, _ := state.LoadTcpUsers()
			st.Users = len(us)
			if m.Port > 0 {
				st.Ports = append(st.Ports, m.Port)
			}
			for _, u := range us {
				if u.Port > 0 {
					st.Ports = append(st.Ports, u.Port)
				}
			}
			st.Ports = dedup(st.Ports)
			res["tcp"] = st
			rows = append(rows, []string{"tcp", "已安装", st.Service, ints(st.Ports), fmt.Sprint(st.Users)})
		} else {
			res["tcp"] = schemeStatus{}
			rows = append(rows, []string{"tcp", "未安装", "-", "-", "0"})
		}
	}
	if want == "all" || want == "udp" {
		if state.UdInstalled() {
			st := schemeStatus{Installed: true, Service: svc("hy-exit")}
			us, _ := state.LoadUdUsers()
			st.Users = len(us)
			for _, u := range us {
				if u.Port > 0 {
					st.Ports = append(st.Ports, u.Port)
				}
			}
			st.Ports = dedup(st.Ports)
			res["udp"] = st
			rows = append(rows, []string{"udp", "已安装", st.Service, ints(st.Ports), fmt.Sprint(st.Users)})
		} else {
			res["udp"] = schemeStatus{}
			rows = append(rows, []string{"udp", "未安装", "-", "-", "0"})
		}
	}

	if c.json {
		ui.JSON(os.Stdout, res)
		return 0
	}
	ui.Table(os.Stdout, []string{"方案", "状态", "服务", "端口", "用户"}, rows)
	return 0
}

// ---------- links ----------
func cmdLinks(rest []string) int {
	if code := guardStateRead(); code != 0 {
		return code
	}
	fs := flag.NewFlagSet("links", flag.ContinueOnError)
	scheme := fs.String("scheme", "all", "tcp|udp|all")
	showQR := fs.Bool("qr", false, "为每条链接打印二维码")
	c, _, err := parseCommon(fs, rest)
	if err != nil {
		return 2
	}
	type item struct {
		Scheme string `json:"scheme"`
		Name   string `json:"name"`
		Port   int    `json:"port"`
		URL    string `json:"url"`
	}
	var items []item
	sc := normScheme(*scheme)

	if sc == "all" || sc == "tcp" {
		if state.TcpInstalled() {
			m, _ := state.LoadTcpMeta()
			us, _ := state.LoadTcpUsers()
			for _, u := range us {
				p := u.Port
				if p == 0 {
					p = m.Port
				}
				items = append(items, item{"tcp", u.Name, p, links.Vless(m, u)})
			}
		}
	}
	if sc == "all" || sc == "udp" {
		if state.UdInstalled() {
			m, _ := state.LoadUdMeta()
			ip := m.IP
			if ip == "" {
				if tm, err := state.LoadTcpMeta(); err == nil {
					ip = tm.PubIP
				}
			}
			us, _ := state.LoadUdUsers()
			for _, u := range us {
				items = append(items, item{"udp", u.Name, u.Port, links.Hysteria2(ip, m, u)})
			}
		}
	}
	if len(items) == 0 {
		fmt.Println("两套方案都未安装（若本机已部署，请用 sudo 运行）")
		return 1
	}
	if c.json {
		ui.JSON(os.Stdout, items)
		return 0
	}
	cur := ""
	for _, it := range items {
		if it.Scheme != cur {
			cur = it.Scheme
			if cur == "tcp" {
				fmt.Println("== TCP / VLESS + Reality ==")
			} else {
				fmt.Println("== UDP / Hysteria2 ==")
			}
		}
		fmt.Printf("  [%s] %d\n    %s\n", it.Name, it.Port, it.URL)
		if *showQR {
			printQR(it.URL)
		}
	}
	return 0
}

// ---------- qr ----------
// cmdQR 给任意文本（比如经 A 中转的链接）打印终端二维码。
func cmdQR(rest []string) int {
	if len(rest) == 0 {
		return fail2("用法: rxctl qr <链接或文本>")
	}
	printQR(strings.Join(rest, " "))
	return 0
}

// ---------- usage ----------
func cmdUsage(rest []string) int {
	if code := guardStateRead(); code != 0 {
		return code
	}
	fs := flag.NewFlagSet("usage", flag.ContinueOnError)
	refresh := fs.Bool("refresh", false, "先累计一次再输出")
	month := fs.String("month", "", "指定月份 YYYY-MM")
	scheme := fs.String("scheme", "all", "tcp|udp|all")
	c, _, err := parseCommon(fs, rest)
	if err != nil {
		return 2
	}
	if *refresh {
		if _, err := engine.QuotaCheck(); err != nil {
			return fail(err.Error())
		}
	}
	rows, err := engine.UsageRows(*month)
	if err != nil {
		fmt.Println("没有用量记录（未安装或尚未累计）")
		return 1
	}
	sc := normScheme(*scheme)
	var filtered []engine.Row
	for _, r := range rows {
		if sc == "all" || r.Scheme == sc {
			filtered = append(filtered, r)
		}
	}
	if c.json {
		ui.JSON(os.Stdout, filtered)
		return 0
	}
	printRows(filtered)
	return 0
}

// ---------- doctor ----------
func cmdDoctor(rest []string) int {
	if code := guardStateRead(); code != 0 {
		return code
	}
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	selftest := fs.Bool("selftest", false, "跑一次真实出网自检")
	c, _, err := parseCommon(fs, rest)
	if err != nil {
		return 2
	}
	type chk struct {
		Item string `json:"item"`
		OK   bool   `json:"ok"`
		Info string `json:"info"`
	}
	var out []chk
	add := func(item string, ok bool, info string) { out = append(out, chk{item, ok, info}) }

	add("系统", true, fmt.Sprintf("%s · kernel %s · systemd %s",
		nz(check.OSRelease(), runtime.GOOS), nz(check.Kernel(), "-"), nz(check.SystemdVersion(), "-")))

	mem := check.Run("sh", "-c", "free -m | awk '/Mem:/{print $2\"M total, \"$7\"M avail\"}'")
	swap := check.Run("sh", "-c", "free -m | awk '/Swap:/{print $2\"M\"}'")
	disk := check.Run("sh", "-c", "df -h / | awk 'NR==2{print $4\" free (\"$5\" used)\"}'")
	add("资源", true, fmt.Sprintf("%s · swap %s · disk %s", nz(mem, "?"), nz(swap, "?"), nz(disk, "?")))

	sbVer, sbPath := check.SingboxVersion()
	add("sing-box", sbVer != "", fmt.Sprintf("%s (%s)", firstNonEmpty(sbVer, "未找到"), sbPath))

	if check.Has("firewall-cmd") {
		add("firewalld", true, check.Run("firewall-cmd", "--state"))
	} else if check.Has("ufw") {
		add("ufw", true, check.Run("ufw", "status"))
	} else {
		add("本机防火墙", true, "未装 firewalld/ufw（依赖云安全组 + iptables）")
	}

	if _, err := os.Stat(state.Path("/etc/systemd/system/rxctl-agent.service")); err == nil {
		ok := check.ServiceActive("rxctl-agent")
		add("agent", ok, fmt.Sprintf("服务 %s（%s）", svc("rxctl-agent"), nz(agentLastTick(), "还没跑过")))
	} else if _, err := os.Stat(state.Path("/etc/cron.d/rxctl-quota")); err == nil {
		add("定时任务", true, "cron 每 10 分钟（未启用 agent）")
	}

	for _, s := range []struct{ name, unit string }{{"tcp", "reality-exit"}, {"udp", "hy-exit"}} {
		installed := (s.name == "tcp" && state.TcpInstalled()) || (s.name == "udp" && state.UdInstalled())
		if !installed {
			add(s.name, true, "未安装")
			continue
		}
		okSvc := check.ServiceActive(s.unit)
		note := fmt.Sprintf("服务 %s", svc(s.unit))
		if !okSvc {
			note = "服务未运行"
		}
		add(s.name, okSvc, note)

		dir := state.TcpDir
		if s.name == "udp" {
			dir = state.UdpDir
		}
		cfg := state.Path(dir + "/config.json")
		mode := check.FileMode(cfg)
		okCfg := check.Run("sing-box", "check", "-c", cfg) == ""
		add(s.name+" 配置", okCfg, fmt.Sprintf("%s · 权限 %s", okCfgWord(okCfg), nz(mode, "?")))
	}

	// 端口监听
	if state.TcpInstalled() {
		if m, err := state.LoadTcpMeta(); err == nil && m.Port > 0 {
			add("tcp 端口", check.PortListening("tcp", m.Port), fmt.Sprintf("%d %s", m.Port, listenWord(check.PortListening("tcp", m.Port))))
		}
	}
	if state.UdInstalled() {
		for _, u := range state.UsersOf("udp") {
			if u.Port > 0 {
				ok := check.PortListening("udp", u.Port)
				add("udp 端口", ok, fmt.Sprintf("%d %s", u.Port, listenWord(ok)))
			}
		}
	}

	// 用户一致性
	for _, sc := range []string{"tcp", "udp"} {
		installed := (sc == "tcp" && state.TcpInstalled()) || (sc == "udp" && state.UdInstalled())
		if !installed {
			continue
		}
		seen := map[int]string{}
		dup := ""
		n := 0
		for _, u := range state.UsersOf(sc) {
			n++
			if u.Port == 0 {
				continue
			}
			if prev, ok := seen[u.Port]; ok {
				dup = fmt.Sprintf("%d（%s/%s）", u.Port, prev, u.Name)
			}
			seen[u.Port] = u.Name
		}
		info := fmt.Sprintf("%d 个用户", n)
		if dup != "" {
			info += "，重复端口 " + dup
		}
		add(sc+" 用户", dup == "", info)
	}

	if *selftest {
		for _, r := range engine.SelfTest("all") {
			info := r.Note
			if r.ExitIP != "" {
				info = "出口 " + r.ExitIP
			}
			add("自检-"+r.Scheme, r.OK, info)
		}
	}

	bad := 0
	for _, x := range out {
		if !x.OK {
			bad++
		}
	}
	if c.json {
		ui.JSON(os.Stdout, out)
	} else {
		for _, x := range out {
			mark := "OK"
			if !x.OK {
				mark = "FAIL"
			}
			fmt.Printf("%-12s %-5s %s\n", x.Item, mark, x.Info)
		}
	}
	if bad > 0 {
		return 5
	}
	return 0
}

// ---------- completion ----------
func cmdCompletion(rest []string) int {
	shell := "bash"
	if len(rest) > 0 {
		shell = rest[0]
	}
	cmds := "install status start stop restart selftest logs user links usage quota port uninstall doctor version help hy2 completion"
	subs := "add del list show rotate enable disable port set check cron uncron status"
	switch shell {
	case "bash", "zsh":
		fmt.Printf(`_rxctl() {
  local cur="${COMP_WORDS[COMP_CWORD]}"
  local cmds="%s"
  local subs="%s"
  if [ $COMP_CWORD -eq 1 ]; then COMPREPLY=( $(compgen -W "$cmds" -- "$cur") ); else COMPREPLY=( $(compgen -W "$subs $cmds" -- "$cur") ); fi
}
complete -F _rxctl rxctl
`, cmds, subs)
	case "fish":
		for _, c := range strings.Fields(cmds) {
			fmt.Printf("complete -c rxctl -n '__fish_use_subcommand' -a '%s'\n", c)
		}
	case "powershell":
		fmt.Printf("Register-ArgumentCompleter -Native -CommandName rxctl -ScriptBlock { param($w,$p,$c) '%s'.Split(' ') | Where-Object { $_ -like \"$w*\" } }\n", cmds)
	default:
		return fail2("支持的 shell: bash | zsh | fish | powershell")
	}
	return 0
}

// ---------- helpers ----------
func svc(unit string) string {
	if check.ServiceActive(unit) {
		return "active"
	}
	if check.Has("systemctl") {
		if s := check.Run("systemctl", "is-enabled", unit); s != "" {
			return "inactive"
		}
	}
	return "not-found"
}

func (s schemeStatus) nameConf() string { return "" }

func osName() string { return runtime.GOOS }

func archName() string { return runtime.GOARCH }

func dedup(in []int) []int { return sortedPorts(in) }

func ints(in []int) string {
	if len(in) == 0 {
		return "-"
	}
	return strings.Trim(strings.Join(strings.Fields(fmt.Sprint(in)), ","), "[]")
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func nz(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

func listenWord(ok bool) string {
	if ok {
		return "listening"
	}
	return "not-listening"
}

func okCfgWord(ok bool) string {
	if ok {
		return "校验 OK"
	}
	return "校验失败"
}
