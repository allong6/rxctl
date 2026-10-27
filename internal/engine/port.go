package engine

import (
	"fmt"
	"strconv"
	"strings"

	"rxctl/internal/state"
)

// ChangePort 修改某方案的默认端口。
// 走默认端口的用户（port=0 或 port 等于旧默认端口）一起迁移；显式指定其它端口的用户不动。
// 会重建配置、重启服务，并把计数规则从旧端口挪到新端口。
func ChangePort(scheme string, newPort int) error {
	if newPort <= 0 || newPort > 65535 {
		return fmt.Errorf("端口不合法: %d", newPort)
	}
	switch scheme {
	case "tcp":
		m, err := state.LoadTcpMeta()
		if err != nil {
			return fmt.Errorf("tcp 未安装")
		}
		old := m.Port
		if old == newPort {
			return nil
		}
		us, _ := state.LoadTcpUsers()
		for i := range us {
			if us[i].Port == 0 || us[i].Port == old {
				us[i].Port = newPort
			}
		}
		if err := SaveMeta(sp(state.TcpDir+"/meta.env"), [][2]string{
			{"PUB_IP", m.PubIP}, {"PORT", itoa(newPort)}, {"SNI", m.SNI},
			{"PRIV", m.Priv}, {"PUB", m.Pub}, {"SID", m.SID},
		}); err != nil {
			return err
		}
		if err := writeUsersTcp(us); err != nil {
			return err
		}
		if err := RegenTcp(); err != nil {
			return err
		}
		if err := Svc("restart", "tcp"); err != nil {
			return err
		}
		FwUnwatchPort("tcp", old)
		FwWatchPort("tcp", newPort)
		FwOpen("tcp", newPort)
		return nil
	case "udp":
		m, err := state.LoadUdMeta()
		if err != nil {
			return fmt.Errorf("udp 未安装")
		}
		old := m.Port
		if old == newPort {
			return nil
		}
		us, _ := state.LoadUdUsers()
		for i := range us {
			if us[i].Port == 0 || us[i].Port == old {
				us[i].Port = newPort
			}
		}
		if err := SaveMeta(sp(state.UdpDir+"/meta.env"), [][2]string{
			{"HY_SNI", m.SNI}, {"HY_IP", m.IP}, {"HY_PORT", itoa(newPort)},
			{"HY_CERT", m.Cert}, {"HY_KEY", m.Key},
		}); err != nil {
			return err
		}
		if err := writeUsersUdp(us); err != nil {
			return err
		}
		if err := RegenUdp(); err != nil {
			return err
		}
		if err := Svc("restart", "udp"); err != nil {
			return err
		}
		FwUnwatchPort("udp", old)
		FwWatchPort("udp", newPort)
		FwOpen("udp", newPort)
		return nil
	}
	return fmt.Errorf("未知方案: %s", scheme)
}

func writeUsersTcp(us []state.TcpUser) error {
	var b strings.Builder
	for _, u := range us {
		fmt.Fprintf(&b, "%s\t%s\t%d\t%d\n", u.Name, u.UUID, u.Port, u.Quota)
	}
	return state.WriteUsers("tcp", []byte(b.String()))
}

func writeUsersUdp(us []state.UdUser) error {
	var b strings.Builder
	for _, u := range us {
		fmt.Fprintf(&b, "%s\t%s\t%d\t%d\t%s\n", u.Name, u.Password, u.Port, u.Quota, u.Obfs)
	}
	return state.WriteUsers("udp", []byte(b.String()))
}

func itoa64(i int64) string { return strconv.FormatInt(i, 10) }
