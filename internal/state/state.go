// Package state 读取/写入运行状态文件。
// 兼容 shell 版（reality-exit.sh）的文件布局，便于两版互切。
package state

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Root 是配置根目录；空表示系统根（测试时用 --root 指向临时目录）。
var Root string

// 目录常量（相对 Root 解析）
const (
	TcpDir = "/etc/reality-exit"
	UdpDir = "/etc/hy"
)

func path(rel string) string {
	if Root == "" {
		return rel
	}
	return filepath.Join(Root, rel)
}

// TcpMeta 是 TCP 方案（VLESS+Reality）的全局参数。
type TcpMeta struct {
	PubIP string
	Port  int
	SNI   string
	Priv  string
	Pub   string
	SID   string
}

// TcpUser 是一个 TCP 用户。
type TcpUser struct {
	Name  string
	UUID  string
	Port  int   // 0 = 走默认端口
	Quota int64 // 0 = 不计量
}

// UdMeta 是 UDP 方案（Hysteria2）的全局参数。
type UdMeta struct {
	SNI  string
	Cert string
	Key  string
	IP   string // 用于生成链接
	Port int    // 默认端口（install 时写入，port 命令可改）
}

// UdUser 是一个 UDP 用户。
type UdUser struct {
	Name     string
	Password string
	Port     int
	Quota    int64
	Obfs     string
}

// Usage 是一行用量记录。
type Usage struct {
	Name    string `json:"name"`
	Port    int    `json:"port"`
	Quota   int64  `json:"quota"`
	Month   string `json:"month"`
	Out     int64  `json:"out"`      // 累计出方向（用户下载）
	In      int64  `json:"in"`       // 累计入方向（用户上传）
	LastOut int64  `json:"last_out"` // 上次读取的内核计数
	LastIn  int64  `json:"last_in"`
	Blocked bool   `json:"blocked"`
}

// User 是跨方案统一的用户视图（用于配额与用量）。
type User struct {
	Name  string
	Port  int
	Quota int64
}

// keyvals 读 key=value 文件（忽略注释与空行）。
func keyvals(p string) (map[string]string, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		out[strings.TrimSpace(k)] = strings.Trim(strings.TrimSpace(v), "\"'")
	}
	return out, sc.Err()
}

func atoi(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}

// LoadTcpMeta 读 /etc/reality-exit/meta.env。
func LoadTcpMeta() (TcpMeta, error) {
	m, err := keyvals(path(TcpDir + "/meta.env"))
	if err != nil {
		return TcpMeta{}, err
	}
	return TcpMeta{
		PubIP: m["PUB_IP"], Port: atoi(m["PORT"]), SNI: m["SNI"],
		Priv: m["PRIV"], Pub: m["PUB"], SID: m["SID"],
	}, nil
}

// TcpInstalled 判断 TCP 方案是否已安装。
func TcpInstalled() bool {
	_, err := os.Stat(path(TcpDir + "/meta.env"))
	return err == nil
}

// PermissionDenied 判断状态目录存在但当前用户读不了（典型：非 root 读 700 目录）。
// 用来把"没装"和"没权限"区分开，避免误报。
func PermissionDenied() bool {
	for _, d := range []string{TcpDir, UdpDir} {
		p := path(d)
		fi, err := os.Stat(p)
		if err != nil || !fi.IsDir() {
			continue
		}
		if _, err := os.ReadDir(p); err != nil && os.IsPermission(err) {
			return true
		}
	}
	return false
}

// LoadTcpUsers 读 users.tsv：
// name<TAB>uuid[<TAB>port[<TAB>quota_bytes]]
func LoadTcpUsers() ([]TcpUser, error) {
	f, err := os.Open(path(TcpDir + "/users.tsv"))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []TcpUser
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		c := strings.Split(line, "\t")
		u := TcpUser{Name: c[0]}
		if len(c) > 1 {
			u.UUID = c[1]
		}
		if len(c) > 2 {
			u.Port = atoi(c[2])
		}
		if len(c) > 3 {
			u.Quota, _ = strconv.ParseInt(strings.TrimSpace(c[3]), 10, 64)
		}
		if u.Name != "" {
			out = append(out, u)
		}
	}
	return out, sc.Err()
}

// LoadUdMeta 读 /etc/hy/meta.env。
func LoadUdMeta() (UdMeta, error) {
	m, err := keyvals(path(UdpDir + "/meta.env"))
	if err != nil {
		return UdMeta{}, err
	}
	return UdMeta{SNI: m["HY_SNI"], Cert: m["HY_CERT"], Key: m["HY_KEY"], IP: m["HY_IP"], Port: atoi(m["HY_PORT"])}, nil
}

// UdInstalled 判断 UDP 方案是否已安装。
func UdInstalled() bool {
	_, err := os.Stat(path(UdpDir + "/meta.env"))
	return err == nil
}

// LoadUdUsers 读 /etc/hy/users.tsv：
// name<TAB>password[<TAB>port[<TAB>quota[<TAB>obfs]]]
// 若没有 users.tsv 但 meta.env 里有 HY_PASS（shell 版单口令），则视为 default 用户。
func LoadUdUsers() ([]UdUser, error) {
	meta, _ := keyvals(path(UdpDir + "/meta.env"))
	f, err := os.Open(path(UdpDir + "/users.tsv"))
	if err != nil {
		if meta["HY_PASS"] == "" {
			return nil, err
		}
		return []UdUser{{
			Name: "default", Password: meta["HY_PASS"],
			Port: atoi(meta["HY_PORT"]), Obfs: meta["HY_OBFS"],
		}}, nil
	}
	defer f.Close()
	var out []UdUser
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		c := strings.Split(line, "\t")
		u := UdUser{Name: c[0]}
		if len(c) > 1 {
			u.Password = c[1]
		}
		if len(c) > 2 {
			u.Port = atoi(c[2])
		}
		if len(c) > 3 {
			u.Quota, _ = strconv.ParseInt(strings.TrimSpace(c[3]), 10, 64)
		}
		if len(c) > 4 {
			u.Obfs = c[4]
		}
		if u.Name != "" {
			out = append(out, u)
		}
	}
	return out, sc.Err()
}

// LoadUsage 读 <scheme>/usage.tsv：
// name<TAB>port<TAB>quota<TAB>month<TAB>out<TAB>in<TAB>last_out<TAB>last_in<TAB>blocked
func LoadUsage(scheme string) ([]Usage, error) {
	dir := TcpDir
	if scheme == "udp" {
		dir = UdpDir
	}
	f, err := os.Open(path(dir + "/usage.tsv"))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []Usage
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		c := strings.Split(line, "\t")
		if len(c) < 9 {
			continue
		}
		u := Usage{Name: c[0], Port: atoi(c[1]), Month: c[3], Blocked: c[8] == "1"}
		u.Quota, _ = strconv.ParseInt(c[2], 10, 64)
		u.Out, _ = strconv.ParseInt(c[4], 10, 64)
		u.In, _ = strconv.ParseInt(c[5], 10, 64)
		u.LastOut, _ = strconv.ParseInt(c[6], 10, 64)
		u.LastIn, _ = strconv.ParseInt(c[7], 10, 64)
		out = append(out, u)
	}
	return out, sc.Err()
}

// WriteUsage 原子写 <scheme>/usage.tsv（600）。
func WriteUsage(scheme string, data []byte) error {
	dir := TcpDir
	if scheme == "udp" {
		dir = UdpDir
	}
	p := path(dir + "/usage.tsv")
	if err := os.MkdirAll(path(dir), 0o700); err != nil {
		return err
	}
	tmp := p + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
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
	return os.Rename(tmp, p)
}

// DropUsage 删除某用户的用量记录。
func DropUsage(scheme, name string) error {
	us, err := LoadUsage(scheme)
	if err != nil {
		return err
	}
	var b strings.Builder
	for _, u := range us {
		if u.Name == name {
			continue
		}
		blk := 0
		if u.Blocked {
			blk = 1
		}
		fmt.Fprintf(&b, "%s\t%d\t%d\t%s\t%d\t%d\t%d\t%d\t%d\n",
			u.Name, u.Port, u.Quota, u.Month, u.Out, u.In, u.LastOut, u.LastIn, blk)
	}
	return WriteUsage(scheme, []byte(b.String()))
}

// WriteUsers 原子写 <scheme>/users.tsv（600）。
func WriteUsers(scheme string, data []byte) error {
	dir := TcpDir
	if scheme == "udp" {
		dir = UdpDir
	}
	p := path(dir + "/users.tsv")
	if err := os.MkdirAll(path(dir), 0o700); err != nil {
		return err
	}
	tmp := p + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
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
	return os.Rename(tmp, p)
}

// UsersOf 返回某方案的用户（统一视图）。
func UsersOf(scheme string) []User {
	var out []User
	if scheme == "tcp" {
		us, _ := LoadTcpUsers()
		for _, u := range us {
			out = append(out, User{u.Name, u.Port, u.Quota})
		}
		return out
	}
	us, _ := LoadUdUsers()
	for _, u := range us {
		out = append(out, User{u.Name, u.Port, u.Quota})
	}
	return out
}

// Human 把字节数转成人类可读。
func Human(b int64) string {
	f := float64(b)
	switch {
	case b >= 1<<40:
		return fmt.Sprintf("%.1fT", f/(1<<40))
	case b >= 1<<30:
		return fmt.Sprintf("%.1fG", f/(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1fM", f/(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1fK", f/(1<<10))
	default:
		return fmt.Sprintf("%dB", b)
	}
}

// ParseSize 解析 500G / 100M / 123（字节）。
func ParseSize(s string) (int64, error) {
	s = strings.TrimSpace(strings.ToUpper(s))
	if s == "" || s == "0" || s == "NONE" || s == "OFF" {
		return 0, nil
	}
	mult := int64(1)
	switch {
	case strings.HasSuffix(s, "TB"):
		mult, s = 1<<40, strings.TrimSuffix(s, "TB")
	case strings.HasSuffix(s, "GB"):
		mult, s = 1<<30, strings.TrimSuffix(s, "GB")
	case strings.HasSuffix(s, "MB"):
		mult, s = 1<<20, strings.TrimSuffix(s, "MB")
	case strings.HasSuffix(s, "KB"):
		mult, s = 1<<10, strings.TrimSuffix(s, "KB")
	case strings.HasSuffix(s, "T"):
		mult, s = 1<<40, strings.TrimSuffix(s, "T")
	case strings.HasSuffix(s, "G"):
		mult, s = 1<<30, strings.TrimSuffix(s, "G")
	case strings.HasSuffix(s, "M"):
		mult, s = 1<<20, strings.TrimSuffix(s, "M")
	case strings.HasSuffix(s, "K"):
		mult, s = 1<<10, strings.TrimSuffix(s, "K")
	case strings.HasSuffix(s, "B"):
		s = strings.TrimSuffix(s, "B")
	}
	n, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0, fmt.Errorf("bad size: %q", s)
	}
	return int64(n * float64(mult)), nil
}

// Path 把相对/绝对路径按 Root 解析（供 engine 使用）。
func Path(rel string) string { return path(rel) }

// DirOf 返回路径所在目录。
func DirOf(p string) string { return filepath.Dir(p) }
