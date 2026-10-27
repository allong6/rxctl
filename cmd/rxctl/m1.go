package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"

	"github.com/allong6/rxctl/internal/check"
	"github.com/allong6/rxctl/internal/engine"
	"github.com/allong6/rxctl/internal/links"
	qr "github.com/allong6/rxctl/internal/qr"
	"github.com/allong6/rxctl/internal/state"
	"github.com/allong6/rxctl/internal/ui"
)

// ---------- install ----------
func cmdInstall(rest []string) int {
	if len(rest) == 0 || strings.HasPrefix(rest[0], "-") {
		fmt.Fprintln(os.Stderr, "用法: rxctl install <tcp|udp|all> [--port N] [--udp-port N] [--sni HOST] [--user NAME] [--quota 500G] [--open-firewall] [--dry-run]")
		return 2
	}
	scheme := normScheme(rest[0])
	fs := flag.NewFlagSet("install", flag.ContinueOnError)
	port := fs.Int("port", 0, "端口（tcp 默认 443，udp 默认 24446）")
	udpPort := fs.Int("udp-port", 0, "install all 时 UDP 端口（默认 24446）")
	sni := fs.String("sni", "www.microsoft.com", "伪装站点 / 证书 CN")
	user := fs.String("user", "default", "初始用户名")
	quotaS := fs.String("quota", "", "初始用户配额（如 1.5T）")
	dry := fs.Bool("dry-run", false, "只打印计划")
	openFw := fs.Bool("open-firewall", false, "本机防火墙放行")
	c, _, err := parseCommon(fs, rest[1:])
	if err != nil {
		return 2
	}
	_ = c
	quota, err := state.ParseSize(*quotaS)
	if err != nil {
		return fail2("配额格式错误: " + err.Error())
	}
	if scheme != "tcp" && scheme != "udp" && scheme != "all" {
		return fail2("未知方案: " + scheme)
	}
	if os.Geteuid() != 0 && !*dry {
		fmt.Fprintln(os.Stderr, "需要 root（读写 600 权限的凭据文件）")
		return 3
	}

	ok := 0
	tcpPort := *port
	if tcpPort == 0 {
		tcpPort = 443
	}
	udpTarget := *port
	if udpTarget == 0 {
		udpTarget = 24446
	}
	if *udpPort > 0 {
		udpTarget = *udpPort
	}
	if scheme == "all" && tcpPort == udpTarget {
		return fail2("TCP 与 UDP 不能用同一个端口，请用 --udp-port 指定另一个")
	}
	if scheme == "tcp" || scheme == "all" {
		p := tcpPort
		if err := engine.InstallTcp(p, *sni, *user, quota, *dry); err != nil {
			fmt.Fprintln(os.Stderr, "安装 tcp 失败:", err)
			return 4
		}
		if *openFw && !*dry {
			fmt.Println("防火墙:", engine.FwOpen("tcp", p))
		}
		if !*dry {
			fmt.Println("OK tcp 已安装，端口", p)
		}
		ok++
	}
	if scheme == "udp" || scheme == "all" {
		p := udpTarget
		if err := engine.InstallUdp(p, *sni, *user, quota, *dry); err != nil {
			fmt.Fprintln(os.Stderr, "安装 udp 失败:", err)
			return 4
		}
		if *openFw && !*dry {
			fmt.Println("防火墙:", engine.FwOpen("udp", p))
		}
		if !*dry {
			fmt.Println("OK udp 已安装，端口", p, "/udp")
		}
		ok++
	}
	if *dry {
		return 0
	}
	if ok > 0 {
		fmt.Println("\n下一步: rxctl links        # 取客户端链接")
		fmt.Println("         rxctl selftest     # 真实出网自检")
	}
	return 0
}

// ---------- hy2（UDP 的别名入口，对齐 shell 版习惯） ----------
func cmdHy2(rest []string) int {
	if len(rest) == 0 {
		fmt.Print(`用法: rxctl hy2 <subcommand>

  install  [--port N] [--sni HOST] [--user NAME] [--quota 500G] [--dry-run]
  info                    链接 + 二维码
  status                  状态
  restart                 重启
  logs [-f]               日志
  selftest                真实出网自检
  user add|del|list ...   用户管理（等价于 rxctl user ... --scheme udp）
`)
		return 0
	}
	sub := rest[0]
	args := rest[1:]
	switch sub {
	case "install", "setup":
		return cmdInstall(append([]string{"udp"}, args...))
	case "info":
		return cmdLinks([]string{"--scheme", "udp", "--qr"})
	case "status":
		return cmdStatus([]string{"udp"})
	case "start", "stop", "restart":
		return cmdSvcAction(sub, []string{"udp"})
	case "logs", "log":
		return cmdLogs(append([]string{"udp"}, args...))
	case "selftest", "test":
		return cmdSelftest([]string{"udp"})
	case "user":
		return cmdUser(injectScheme(args, "udp"))
	case "uninstall":
		return cmdUninstall(append([]string{"udp"}, args...))
	}
	return fail2("未知 hy2 子命令: " + sub)
}

// injectScheme 给 user 子命令补默认 --scheme（用户显式给了就不动）。
func injectScheme(args []string, scheme string) []string {
	for _, a := range args {
		if a == "--scheme" || strings.HasPrefix(a, "--scheme=") || a == "-scheme" {
			return args
		}
	}
	if len(args) == 0 {
		return []string{"--scheme", scheme}
	}
	out := []string{args[0], "--scheme", scheme}
	return append(out, args[1:]...)
}

// ---------- port ----------
func cmdPort(rest []string) int {
	fs := flag.NewFlagSet("port", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	if err := fs.Parse(rest); err != nil {
		return 2
	}
	a := fs.Args()
	if len(a) < 2 {
		return fail2("用法: rxctl port <tcp|udp> <新端口>")
	}
	scheme := normScheme(a[0])
	if scheme != "tcp" && scheme != "udp" {
		return fail2("方案只能是 tcp 或 udp")
	}
	p, err := strconv.Atoi(a[1])
	if err != nil || p <= 0 || p > 65535 {
		return fail2("端口不合法: " + a[1])
	}
	if err := engine.ChangePort(scheme, p); err != nil {
		return fail(err.Error())
	}
	fmt.Printf("OK %s 默认端口已改为 %d（走默认端口的用户已一起迁移）\n", scheme, p)
	return 0
}

// ---------- user ----------
func cmdUser(rest []string) int {
	if len(rest) == 0 || strings.HasPrefix(rest[0], "-") {
		fmt.Fprintln(os.Stderr, "用法: rxctl user <list|add|del|show|rotate|enable|disable|port> ...")
		return 2
	}
	sub := rest[0]
	args := rest[1:]
	switch sub {
	case "list", "ls":
		return userList(args)
	case "add", "new":
		return userAdd(args)
	case "del", "rm":
		return userDel(args)
	case "show", "get":
		return userShow(args)
	case "rotate", "rot":
		return userRotate(args)
	case "enable", "unblock":
		return userEnable(args, true)
	case "disable", "block":
		return userEnable(args, false)
	case "port":
		return userPort(args)
	}
	fmt.Fprintln(os.Stderr, "未知子命令:", sub)
	return 2
}

func normScheme(s string) string {
	switch strings.ToLower(s) {
	case "t", "tcp":
		return "tcp"
	case "u", "udp", "hy", "hy2":
		return "udp"
	case "a", "all":
		return "all"
	}
	return s
}

type userOpt struct {
	scheme string
	name   string
	port   int
	quota  int64
	pass   string
	obfs   string
	json   bool
	qr     bool
	root   string
}

func parseUser(rest []string) (userOpt, error) {
	var o userOpt
	fs := flag.NewFlagSet("user", flag.ContinueOnError)
	sc := fs.String("scheme", "tcp", "tcp|udp|both")
	p := fs.Int("port", 0, "独立端口")
	q := fs.String("quota", "", "配额（如 500G）")
	pw := fs.String("password", "", "UDP 口令（可选）")
	ob := fs.String("obfs", "", "UDP 混淆口令（可选）")
	j := fs.Bool("json", false, "机器可读")
	qrFlag := fs.Bool("qr", false, "打印二维码")
	rt := fs.String("root", "", "配置根目录（测试用）")
	fs.SetOutput(os.Stderr)
	if err := fs.Parse(rest); err != nil {
		return o, err
	}
	if *rt != "" {
		state.Root = *rt
	}
	o.scheme = normScheme(*sc)
	o.name = fs.Arg(0)
	o.port = *p
	o.pass, o.obfs = *pw, *ob
	o.json, o.qr, o.root = *j, *qrFlag, *rt
	if *q != "" {
		v, err := state.ParseSize(*q)
		if err != nil {
			return o, err
		}
		o.quota = v
	}
	return o, nil
}

func userList(rest []string) int {
	if code := guardStateRead(); code != 0 {
		return code
	}
	fs := flag.NewFlagSet("user list", flag.ContinueOnError)
	j := fs.Bool("json", false, "json")
	sc := fs.String("scheme", "all", "tcp|udp|all")
	rt := fs.String("root", "", "root")
	fs.SetOutput(os.Stderr)
	if err := fs.Parse(rest); err != nil {
		return 2
	}
	if *rt != "" {
		state.Root = *rt
	}
	scheme := normScheme(*sc)
	type row struct {
		Name   string `json:"name"`
		Scheme string `json:"scheme"`
		Port   int    `json:"port"`
		Quota  int64  `json:"quota"`
	}
	var out []row
	if (scheme == "tcp" || scheme == "all") && state.TcpInstalled() {
		us, _ := state.LoadTcpUsers()
		for _, u := range us {
			out = append(out, row{u.Name, "tcp", u.Port, u.Quota})
		}
	}
	if (scheme == "udp" || scheme == "all") && state.UdInstalled() {
		us, _ := state.LoadUdUsers()
		for _, u := range us {
			out = append(out, row{u.Name, "udp", u.Port, u.Quota})
		}
	}
	if *j {
		ui.JSON(os.Stdout, out)
		return 0
	}
	var rows [][]string
	for _, r := range out {
		q := "none"
		if r.Quota > 0 {
			q = state.Human(r.Quota)
		}
		p := fmt.Sprint(r.Port)
		if r.Port == 0 {
			p = "default"
		}
		rows = append(rows, []string{r.Name, r.Scheme, p, q})
	}
	if len(rows) == 0 {
		fmt.Println("没有用户（未安装或未创建）")
		return 1
	}
	ui.Table(os.Stdout, []string{"用户", "方案", "端口", "配额"}, rows)
	return 0
}

func userAdd(rest []string) int {
	nm, rest := splitName(rest)
	o, err := parseUser(rest)
	if err != nil {
		return 2
	}
	o.name = nm
	if o.name == "" {
		return fail2("用法: rxctl user add <名字> [--scheme tcp|udp|both] [--port N] [--quota 500G]")
	}
	if o.scheme == "all" {
		o.scheme = "both"
	}
	wantTcp := o.scheme == "tcp" || o.scheme == "both"
	wantUdp := o.scheme == "udp" || o.scheme == "both"
	if wantTcp && !state.TcpInstalled() {
		return fail("tcp 未安装")
	}
	if wantUdp && !state.UdInstalled() {
		return fail("udp 未安装")
	}
	if wantTcp {
		if err := checkPortFree("tcp", o.name, o.port); err != nil {
			return fail(err.Error())
		}
		uuid := check.Run("sing-box", "generate", "uuid")
		if uuid == "" {
			uuid = engine.RandUUID()
		}
		us, _ := state.LoadTcpUsers()
		us = append(us, state.TcpUser{Name: o.name, UUID: uuid, Port: o.port, Quota: o.quota})
		if err := writeTcpUsers(us); err != nil {
			return fail(err.Error())
		}
		if err := engine.RegenTcp(); err != nil {
			return fail("配置校验失败: " + err.Error())
		}
		_ = engine.Svc("restart", "tcp")
		engine.FwWatchPort("tcp", o.port)
		engine.FwOpen("tcp", o.port)
		fmt.Printf("OK tcp 用户 %s 已创建（端口 %s）\n", o.name, portWord(o.port))
	}
	if wantUdp {
		if err := checkPortFree("udp", o.name, o.port); err != nil {
			return fail(err.Error())
		}
		pass := o.pass
		if pass == "" {
			pass = engine.RandPassword(24)
		}
		us, _ := state.LoadUdUsers()
		us = append(us, state.UdUser{Name: o.name, Password: pass, Port: o.port, Quota: o.quota, Obfs: o.obfs})
		if err := writeUdpUsers(us); err != nil {
			return fail(err.Error())
		}
		if err := engine.RegenUdp(); err != nil {
			return fail("配置校验失败: " + err.Error())
		}
		_ = engine.Svc("restart", "udp")
		engine.FwWatchPort("udp", o.port)
		engine.FwOpen("udp", o.port)
		fmt.Printf("OK udp 用户 %s 已创建（端口 %s/udp）\n", o.name, portWord(o.port))
	}
	return 0
}

// checkPortFree 同一方案内端口不能重复（port=0 表示共享默认端口，不校验）。
func checkPortFree(scheme, name string, port int) error {
	if port == 0 {
		return nil
	}
	if port < 1 || port > 65535 {
		return fmt.Errorf("端口不合法: %d", port)
	}
	def := 0
	if scheme == "tcp" {
		if m, err := state.LoadTcpMeta(); err == nil {
			def = m.Port
		}
		for _, u := range state.UsersOf("tcp") {
			if u.Name == name {
				continue
			}
			if u.Port == port {
				return fmt.Errorf("端口 %d 已被用户 %s 占用", port, u.Name)
			}
		}
	} else {
		if m, err := state.LoadUdMeta(); err == nil {
			def = m.Port
		}
		for _, u := range state.UsersOf("udp") {
			if u.Name == name {
				continue
			}
			if u.Port == port {
				return fmt.Errorf("端口 %d 已被用户 %s 占用", port, u.Name)
			}
		}
	}
	_ = def
	if engine.PortInUse(scheme, port) {
		return fmt.Errorf("%s %d 已被其它进程监听", strings.ToUpper(scheme), port)
	}
	return nil
}

func userDel(rest []string) int {
	nm, rest := splitName(rest)
	o, err := parseUser(rest)
	if err != nil {
		return 2
	}
	o.name = nm
	if o.name == "" {
		return fail2("用法: rxctl user del <名字> [--scheme tcp|udp|both]")
	}
	if o.scheme == "all" {
		o.scheme = "both"
	}
	delTcp := o.scheme == "tcp" || o.scheme == "both"
	delUdp := o.scheme == "udp" || o.scheme == "both"
	if delTcp {
		us, _ := state.LoadTcpUsers()
		if len(us) <= 1 {
			return fail("tcp 只剩一个用户，删掉会让服务无入站；请先加新用户")
		}
		for _, u := range us {
			if u.Name == o.name && u.Port > 0 {
				engine.FwUnwatchPort("tcp", u.Port)
			}
		}
		var keep []state.TcpUser
		for _, u := range us {
			if u.Name != o.name {
				keep = append(keep, u)
			}
		}
		_ = writeTcpUsers(keep)
		_ = engine.RegenTcp()
		_ = engine.Svc("restart", "tcp")
		_ = state.DropUsage("tcp", o.name)
	}
	if delUdp {
		us, _ := state.LoadUdUsers()
		if len(us) <= 1 {
			return fail("udp 只剩一个用户，删掉会让服务无入站；请先加新用户")
		}
		for _, u := range us {
			if u.Name == o.name && u.Port > 0 {
				engine.FwUnwatchPort("udp", u.Port)
			}
		}
		var keep []state.UdUser
		for _, u := range us {
			if u.Name != o.name {
				keep = append(keep, u)
			}
		}
		_ = writeUdpUsers(keep)
		_ = engine.RegenUdp()
		_ = engine.Svc("restart", "udp")
		_ = state.DropUsage("udp", o.name)
	}
	fmt.Println("OK 已删除:", o.name)
	return 0
}

func userShow(rest []string) int {
	if code := guardStateRead(); code != 0 {
		return code
	}
	nm, rest := splitName(rest)
	o, err := parseUser(rest)
	if err != nil {
		return 2
	}
	o.name = nm
	if o.name == "" {
		return fail2("用法: rxctl user show <名字> [--scheme tcp|udp|both] [--json] [--qr]")
	}
	if o.scheme == "all" {
		o.scheme = "both"
	}
	found := false
	if o.scheme == "tcp" || o.scheme == "both" {
		if state.TcpInstalled() {
			m, _ := state.LoadTcpMeta()
			for _, u := range state.UsersOf("tcp") {
				if u.Name != o.name {
					continue
				}
				_ = m
				tu := findTcpUser(o.name)
				if o.json {
					fmt.Println(singboxOutbound(m, tu))
				} else {
					url := links.Vless(m, tu)
					fmt.Println(url)
					if o.qr {
						printQR(url)
					}
				}
				found = true
			}
		}
	}
	if o.scheme == "udp" || o.scheme == "both" {
		if state.UdInstalled() {
			m, _ := state.LoadUdMeta()
			if m.IP == "" {
				if tm, err := state.LoadTcpMeta(); err == nil {
					m.IP = tm.PubIP
				}
			}
			for _, u := range state.UsersOf("udp") {
				if u.Name != o.name {
					continue
				}
				uu := findUdpUser(o.name)
				if o.json {
					fmt.Println(singboxHy2(m, uu))
				} else {
					url := links.Hysteria2(m.IP, m, uu)
					fmt.Println(url)
					if o.qr {
						printQR(url)
					}
				}
				found = true
			}
		}
	}
	if !found {
		return fail("没有这个用户: " + o.name)
	}
	return 0
}

func userRotate(rest []string) int {
	nm, rest := splitName(rest)
	o, err := parseUser(rest)
	if err != nil {
		return 2
	}
	o.name = nm
	if o.name == "" {
		return fail2("用法: rxctl user rotate <名字> [--scheme tcp|udp|both]")
	}
	if o.scheme == "all" {
		o.scheme = "both"
	}
	changed := false
	if o.scheme == "tcp" || o.scheme == "both" {
		us, _ := state.LoadTcpUsers()
		for i := range us {
			if us[i].Name == o.name {
				id := check.Run("sing-box", "generate", "uuid")
				if id == "" {
					id = engine.RandUUID()
				}
				us[i].UUID = id
				changed = true
			}
		}
		if changed {
			_ = writeTcpUsers(us)
			_ = engine.RegenTcp()
			_ = engine.Svc("restart", "tcp")
			fmt.Println("OK tcp 凭据已轮换（旧链接失效）")
		}
	}
	if o.scheme == "udp" || o.scheme == "both" {
		us, _ := state.LoadUdUsers()
		ch := false
		for i := range us {
			if us[i].Name == o.name {
				us[i].Password = engine.RandPassword(24)
				ch = true
			}
		}
		if ch {
			_ = writeUdpUsers(us)
			_ = engine.RegenUdp()
			_ = engine.Svc("restart", "udp")
			fmt.Println("OK udp 口令已轮换")
			changed = true
		}
	}
	if !changed {
		return fail("没有这个用户: " + o.name)
	}
	return 0
}

func userEnable(rest []string, enable bool) int {
	nm, rest := splitName(rest)
	o, err := parseUser(rest)
	if err != nil {
		return 2
	}
	o.name = nm
	if o.name == "" {
		return fail2("用法: rxctl user enable|disable <名字> [--scheme tcp|udp|both]")
	}
	if o.scheme == "all" {
		o.scheme = "both"
	}
	acted := false
	if o.scheme == "tcp" || o.scheme == "both" {
		for _, u := range state.UsersOf("tcp") {
			if u.Name != o.name || u.Port <= 0 {
				continue
			}
			if enable {
				engine.FwUnblock("tcp", u.Port)
				fmt.Println("OK 已解封 TCP", u.Port)
			} else {
				engine.FwBlockPort("tcp", u.Port)
				fmt.Println("OK 已封禁 TCP", u.Port)
			}
			acted = true
		}
	}
	if o.scheme == "udp" || o.scheme == "both" {
		for _, u := range state.UsersOf("udp") {
			if u.Name != o.name || u.Port <= 0 {
				continue
			}
			if enable {
				engine.FwUnblock("udp", u.Port)
				fmt.Println("OK 已解封 UDP", u.Port)
			} else {
				engine.FwBlockPort("udp", u.Port)
				fmt.Println("OK 已封禁 UDP", u.Port)
			}
			acted = true
		}
	}
	if !acted {
		return fail("找不到该用户的可封禁端口（无独立端口的用户不能单独封禁）")
	}
	return 0
}

func userPort(rest []string) int {
	fs := flag.NewFlagSet("user port", flag.ContinueOnError)
	sc := fs.String("scheme", "tcp", "tcp|udp")
	rt := fs.String("root", "", "root")
	fs.SetOutput(os.Stderr)
	if err := fs.Parse(rest); err != nil {
		return 2
	}
	if *rt != "" {
		state.Root = *rt
	}
	a := fs.Args()
	if len(a) < 2 {
		return fail2("用法: rxctl user port <名字> <新端口> [--scheme tcp|udp]")
	}
	name := a[0]
	np, err := strconv.Atoi(a[1])
	if err != nil || np <= 0 || np > 65535 {
		return fail2("端口不合法: " + a[1])
	}
	scheme := normScheme(*sc)
	if scheme != "tcp" && scheme != "udp" {
		return fail2("--scheme 只能是 tcp 或 udp")
	}
	if err := checkPortFree(scheme, name, np); err != nil {
		return fail(err.Error())
	}
	old := 0
	if scheme == "tcp" {
		us, _ := state.LoadTcpUsers()
		for i := range us {
			if us[i].Name == name {
				old = us[i].Port
				us[i].Port = np
			}
		}
		if err := writeTcpUsers(us); err != nil {
			return fail(err.Error())
		}
		if err := engine.RegenTcp(); err != nil {
			return fail(err.Error())
		}
		_ = engine.Svc("restart", "tcp")
	} else {
		us, _ := state.LoadUdUsers()
		for i := range us {
			if us[i].Name == name {
				old = us[i].Port
				us[i].Port = np
			}
		}
		if err := writeUdpUsers(us); err != nil {
			return fail(err.Error())
		}
		if err := engine.RegenUdp(); err != nil {
			return fail(err.Error())
		}
		_ = engine.Svc("restart", "udp")
	}
	if old > 0 {
		engine.FwUnwatchPort(scheme, old)
	}
	engine.FwWatchPort(scheme, np)
	fmt.Printf("OK %s 用户 %s 端口: %d → %d\n", scheme, name, old, np)
	return 0
}

// ---------- quota ----------
func cmdQuota(rest []string) int {
	if code := guardStateRead(); code != 0 {
		return code
	}
	if len(rest) == 0 {
		return fail2("用法: rxctl quota <check|set|cron|uncron|status> ...")
	}
	sub := rest[0]
	switch sub {
	case "check", "run":
		rows, err := engine.QuotaCheck()
		if err != nil {
			return fail(err.Error())
		}
		printRows(rows)
		return 0
	case "status":
		rows, err := engine.UsageRows("")
		if err != nil {
			return fail(err.Error())
		}
		printRows(rows)
		return 0
	case "set":
		if len(rest) < 3 {
			return fail2("用法: rxctl quota set <名字> <大小，如 500G>")
		}
		name := rest[1]
		q, err := state.ParseSize(rest[2])
		if err != nil {
			return fail(err.Error())
		}
		setQuota(state.TcpDir+"/users.tsv", name, q)
		setQuota(state.UdpDir+"/users.tsv", name, q)
		_ = engine.RegenTcp()
		_ = engine.RegenUdp()
		_ = engine.Svc("restart", "tcp")
		_ = engine.Svc("restart", "udp")
		fmt.Println("OK 配额已更新:", name, state.Human(q))
		return 0
	case "cron":
		if os.Geteuid() != 0 && state.Root == "" {
			return fail("需要 root")
		}
		exe, err := os.Executable()
		if err != nil {
			return fail(err.Error())
		}
		body := fmt.Sprintf("SHELL=/bin/bash\nPATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin\n*/10 * * * * root %s quota check >/dev/null 2>&1\n", exe)
		if err := os.WriteFile(state.Path("/etc/cron.d/rxctl-quota"), []byte(body), 0o644); err != nil {
			return fail(err.Error())
		}
		fmt.Println("OK 已安装 /etc/cron.d/rxctl-quota（每 10 分钟）")
		return 0
	case "uncron":
		_ = os.Remove(state.Path("/etc/cron.d/rxctl-quota"))
		fmt.Println("OK 已移除定时任务")
		return 0
	}
	return fail2("未知子命令: " + sub)
}

func printRows(rows []engine.Row) {
	var out [][]string
	for _, r := range rows {
		q := "none"
		if r.Quota > 0 {
			q = state.Human(r.Quota)
		}
		out = append(out, []string{r.Name, r.Scheme, fmt.Sprint(r.Port), state.Human(r.Used), q, r.Percent, r.Status})
	}
	if len(out) == 0 {
		fmt.Println("没有用量记录")
		return
	}
	ui.Table(os.Stdout, []string{"用户", "方案", "端口", "已用", "配额", "占比", "状态"}, out)
}

// ---------- uninstall / 服务控制 ----------
func cmdUninstall(rest []string) int {
	if len(rest) == 0 || strings.HasPrefix(rest[0], "-") {
		return fail2("用法: rxctl uninstall <tcp|udp|all> [--purge]")
	}
	scheme := normScheme(rest[0])
	fs := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	purge := fs.Bool("purge", false, "连配置一起删")
	c, _, err := parseCommon(fs, rest[1:])
	if err != nil {
		return 2
	}
	_ = c
	if err := engine.Uninstall(scheme, *purge); err != nil {
		return fail(err.Error())
	}
	fmt.Println("OK 已卸载:", scheme)
	return 0
}

func cmdSvcAction(action string, rest []string) int {
	if code := guardStateRead(); code != 0 {
		return code
	}
	scheme := "all"
	for _, a := range rest {
		if !strings.HasPrefix(a, "-") {
			scheme = normScheme(a)
			break
		}
	}
	for _, s := range []string{"tcp", "udp"} {
		if scheme != "all" && scheme != s {
			continue
		}
		installed := (s == "tcp" && state.TcpInstalled()) || (s == "udp" && state.UdInstalled())
		if !installed {
			continue
		}
		if err := engine.Svc(action, s); err != nil {
			fmt.Fprintf(os.Stderr, "%s %s 失败: %v\n", action, s, err)
			return 4
		}
		fmt.Printf("OK %s %s\n", action, s)
	}
	return 0
}

// ---------- firewall ----------
// cmdFw 重建本工具的计数/封禁规则（开机或重启 iptables 后调用，幂等）。
func cmdFw(rest []string) int {
	sub := "restore"
	if len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
		sub = rest[0]
	}
	switch sub {
	case "restore":
		if os.Geteuid() != 0 {
			return fail("需要 root")
		}
		engine.FwEnsure()
		n := 0
		for _, sc := range []string{"tcp", "udp"} {
			installed := (sc == "tcp" && state.TcpInstalled()) || (sc == "udp" && state.UdInstalled())
			if !installed {
				continue
			}
			for _, u := range state.UsersOf(sc) {
				if u.Port > 0 {
					engine.FwEnsurePort(sc, u.Port)
					n++
				}
			}
		}
		fmt.Printf("OK 计数链已重建（%d 个端口）\n", n)
		return 0
	case "status":
		for _, c := range []string{engine.ChainOut, engine.ChainIn} {
			fmt.Printf("== %s ==\n%s\n", c, check.Run("iptables", "-S", c))
		}
		return 0
	}
	return fail2("用法: rxctl fw <restore|status>")
}

// ---------- logs / selftest ----------
func cmdLogs(rest []string) int {
	scheme := "all"
	follow := false
	n := 30
	for i := 0; i < len(rest); i++ {
		a := rest[i]
		switch {
		case a == "-f" || a == "--follow":
			follow = true
		case a == "-n" || a == "--lines":
			if i+1 < len(rest) {
				if v, err := strconv.Atoi(rest[i+1]); err == nil {
					n = v
				}
				i++
			}
		case strings.HasPrefix(a, "--lines="):
			if v, err := strconv.Atoi(strings.TrimPrefix(a, "--lines=")); err == nil {
				n = v
			}
		case !strings.HasPrefix(a, "-"):
			scheme = normScheme(a)
		}
	}
	var units []string
	if scheme == "all" || scheme == "tcp" {
		units = append(units, "reality-exit")
	}
	if scheme == "all" || scheme == "udp" {
		units = append(units, "hy-exit")
	}
	for _, u := range units {
		if !check.Has("journalctl") {
			fmt.Println("没有 journalctl")
			return 1
		}
		if follow {
			fmt.Printf("== %s (Ctrl-C 退出) ==\n", u)
			c := exec.Command("journalctl", "-u", u, "-f", "--no-pager")
			c.Stdout, c.Stderr = os.Stdout, os.Stderr
			_ = c.Run()
		} else {
			fmt.Printf("== %s 最近 %d 行 ==\n", u, n)
			fmt.Println(check.Run("journalctl", "-u", u, "-n", strconv.Itoa(n), "--no-pager"))
		}
	}
	return 0
}

func cmdSelftest(rest []string) int {
	if code := guardStateRead(); code != 0 {
		return code
	}
	scheme := "all"
	j := false
	for _, a := range rest {
		switch a {
		case "--json":
			j = true
		default:
			if !strings.HasPrefix(a, "-") {
				scheme = normScheme(a)
			}
		}
	}
	scheme = normScheme(scheme)
	results := engine.SelfTest(scheme)
	bad := 0
	for _, r := range results {
		if !r.OK {
			bad++
		}
	}
	if j {
		ui.JSON(os.Stdout, results)
	} else {
		for _, r := range results {
			mark := "OK"
			if !r.OK {
				mark = "FAIL"
			}
			info := r.Note
			if r.ExitIP != "" {
				info = "出口 " + r.ExitIP
				if r.Want != "" && r.ExitIP != r.Want {
					info += "（记录 " + r.Want + "）"
				}
			}
			fmt.Printf("%-6s %-5s %s\n", r.Scheme, mark, info)
		}
	}
	if bad > 0 {
		return 5
	}
	return 0
}

// ---------- helpers ----------
func writeTcpUsers(us []state.TcpUser) error {
	var b strings.Builder
	for _, u := range us {
		fmt.Fprintf(&b, "%s\t%s\t%d\t%d\n", u.Name, u.UUID, u.Port, u.Quota)
	}
	return state.WriteUsers("tcp", []byte(b.String()))
}

func writeUdpUsers(us []state.UdUser) error {
	var b strings.Builder
	for _, u := range us {
		fmt.Fprintf(&b, "%s\t%s\t%d\t%d\t%s\n", u.Name, u.Password, u.Port, u.Quota, u.Obfs)
	}
	return state.WriteUsers("udp", []byte(b.String()))
}

func findTcpUser(name string) state.TcpUser {
	us, _ := state.LoadTcpUsers()
	for _, u := range us {
		if u.Name == name {
			return u
		}
	}
	return state.TcpUser{Name: name}
}

func findUdpUser(name string) state.UdUser {
	us, _ := state.LoadUdUsers()
	for _, u := range us {
		if u.Name == name {
			return u
		}
	}
	return state.UdUser{Name: name}
}

func singboxOutbound(m state.TcpMeta, u state.TcpUser) string {
	port := u.Port
	if port == 0 {
		port = m.Port
	}
	return fmt.Sprintf(`{
  "type": "vless", "tag": %q, "server": %q, "server_port": %d,
  "uuid": %q, "flow": "xtls-rprx-vision",
  "tls": {"enabled": true, "server_name": %q,
          "utls": {"enabled": true, "fingerprint": "chrome"},
          "reality": {"enabled": true, "public_key": %q, "short_id": %q}}
}`, u.Name, m.PubIP, port, u.UUID, m.SNI, m.Pub, m.SID)
}

func singboxHy2(m state.UdMeta, u state.UdUser) string {
	obfs := ""
	if u.Obfs != "" {
		obfs = fmt.Sprintf(",\n  \"obfs\": {\"type\": \"salamander\", \"password\": %q}", u.Obfs)
	}
	return fmt.Sprintf(`{
  "type": "hysteria2", "tag": %q, "server": %q, "server_port": %d,
  "password": %q%s,
  "tls": {"enabled": true, "insecure": true, "server_name": %q}
}`, u.Name, m.IP, u.Port, u.Password, obfs, m.SNI)
}

func printQR(content string) {
	q, err := qr.New(content, qr.Medium)
	if err != nil {
		fmt.Fprintln(os.Stderr, "二维码生成失败:", err)
		return
	}
	fmt.Println(q.ToSmallString(false))
}

func portWord(p int) string {
	if p == 0 {
		return "default"
	}
	return strconv.Itoa(p)
}

func fail(msg string) int  { fmt.Fprintln(os.Stderr, "错误:", msg); return 1 }
func fail2(msg string) int { fmt.Fprintln(os.Stderr, msg); return 2 }

func appendLine(rel, line string) error {
	p := state.Path(rel)
	if err := os.MkdirAll(state.DirOf(p), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(line)
	return err
}

func readLines(rel string) ([]string, error) {
	b, err := os.ReadFile(state.Path(rel))
	if err != nil {
		return nil, err
	}
	return strings.Split(strings.TrimRight(string(b), "\n"), "\n"), nil
}

func writeLines(rel string, lines []string) error {
	return os.WriteFile(state.Path(rel), []byte(strings.Join(lines, "\n")+"\n"), 0o600)
}

func dropLine(rel, name string) error {
	lines, err := readLines(rel)
	if err != nil {
		return err
	}
	var out []string
	for _, l := range lines {
		if l == "" {
			continue
		}
		if strings.Split(l, "\t")[0] != name {
			out = append(out, l)
		}
	}
	return writeLines(rel, out)
}

func replaceField(rel, name string, idx int, val string) error {
	lines, err := readLines(rel)
	if err != nil {
		return err
	}
	for i, l := range lines {
		if l == "" {
			continue
		}
		c := strings.Split(l, "\t")
		if c[0] != name {
			continue
		}
		for len(c) <= idx {
			c = append(c, "")
		}
		c[idx] = val
		lines[i] = strings.Join(c, "\t")
	}
	return writeLines(rel, lines)
}

func setQuota(rel, name string, q int64) {
	lines, err := readLines(rel)
	if err != nil {
		return
	}
	for i, l := range lines {
		if l == "" {
			continue
		}
		c := strings.Split(l, "\t")
		if c[0] != name {
			continue
		}
		for len(c) <= 3 {
			c = append(c, "")
		}
		c[3] = fmt.Sprint(q)
		lines[i] = strings.Join(c, "\t")
	}
	_ = writeLines(rel, lines)
}

func splitName(rest []string) (string, []string) {
	for i, a := range rest {
		if !strings.HasPrefix(a, "-") {
			out := append([]string{}, rest[:i]...)
			out = append(out, rest[i+1:]...)
			return a, out
		}
	}
	return "", rest
}

func sortedPorts(ps []int) []int {
	seen := map[int]bool{}
	var out []int
	for _, p := range ps {
		if p <= 0 || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	sort.Ints(out)
	return out
}
