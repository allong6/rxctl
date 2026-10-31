# rxctl

[![ci](https://github.com/allong6/rxctl/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/allong6/rxctl/actions/workflows/ci.yml)
[![license: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

代理出口服务器管理工具：**一个静态二进制**（Go，无运行时依赖）管完
**安装 → 用户/端口/配额 → 链接/二维码 → 自检 → 自愈 → 卸载**。

> 本仓库是**通用工具**，不包含任何内部配置、真实网络地址或凭据；示例中的 IP、端口、用户名均为占位符。

支持两套方案，可只装一套，也可同机并存：

| 方案 | 协议 | 实现 | 凭据 | 证书 | 端口 |
|---|---|---|---|---|---|
| `tcp` | VLESS + Reality | sing-box `vless` 入站 | UUID | 不需要 | TCP |
| `udp` | Hysteria2（QUIC） | sing-box `hysteria2` 入站 | 口令（可选 obfs） | 自签 | UDP |

> 状态：**M1 + M2 完成**（v0.4.8）。M3（配置中心/自更新）未开始，见 [docs/DESIGN.md](docs/DESIGN.md)。

---

## 1. 30 秒上手

```bash
# 装 TCP：443 给 alice（100G/月），再给 bob 开 8443（50G/月）
sudo rxctl install tcp --port 443 --sni www.microsoft.com --user alice --quota 100G
sudo rxctl user add bob --port 8443 --quota 50G

sudo rxctl links            # 取 vless:// 链接，导入客户端
sudo rxctl links --qr       # 直接扫码
sudo rxctl selftest         # 真实出网自检（本机临时客户端 → ip.sb）
sudo rxctl agent install    # 常驻：定时统计 + 服务自愈（替代 cron）
```

> **权限**：`/etc/reality-exit`、`/etc/hy` 是 `700 root`，**除 `version/help` 外所有命令都要 `sudo`**。
> 不用 sudo 时会明确提示"需要 root"，不会误报"未安装"。
> 若 `sudo rxctl` 报 `command not found`（CentOS 的 sudo `secure_path` 不含 `/usr/local/bin`），
> 用绝对路径 `sudo /usr/local/bin/rxctl …`；已建 `/usr/bin/rxctl` 软链时 `sudo rxctl` 可直接用。

### 1.1 先把 rxctl 放到服务器上（安装 / 位置）

rxctl 是**单个静态二进制**，没有安装包、没有运行时依赖。推荐固定放在 **`/usr/local/bin/rxctl`**。

**方式 A：本地交叉编译后拷过去**（最常用）

```bash
# 构建机（Linux/macOS/Windows 均可）
make cross                    # 产出 dist/rxctl-linux-amd64 等
# 或只编一个
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath \
  -ldflags "-s -w" -o dist/rxctl-linux-amd64 ./cmd/rxctl

scp dist/rxctl-linux-amd64 root@<服务器>:/usr/local/bin/rxctl
ssh root@<服务器> 'chmod 755 /usr/local/bin/rxctl && rxctl -V'
```

**方式 B：直接在服务器上构建**

```bash
git clone https://github.com/allong6/rxctl.git
cd rxctl
CGO_ENABLED=0 go build -trimpath -o /usr/local/bin/rxctl ./cmd/rxctl
```

**方式 C：`go install`**

```bash
go install github.com/allong6/rxctl/cmd/rxctl@latest    # → $GOPATH/bin/rxctl
sudo install -m 0755 "$(go env GOPATH)/bin/rxctl" /usr/local/bin/rxctl
```

**方式 D：从 GitHub Release 下载**（仓库已 public，直接下载）

```bash
curl -LO https://github.com/allong6/rxctl/releases/download/v0.4.8/rxctl-linux-amd64
curl -LO https://github.com/allong6/rxctl/releases/download/v0.4.8/SHA256SUMS
sha256sum -c SHA256SUMS
sudo install -m 0755 rxctl-linux-amd64 /usr/local/bin/rxctl
```

Release 资产：`rxctl-linux-amd64`（x86_64）、`rxctl-linux-arm64`（aarch64）、
`rxctl-linux-armv7`（armhf/armv7）、`SHA256SUMS`。

**sudo 入口（`install` 自动处理）**

`rxctl install …` 会先判断 `sudo` 能否找到 `rxctl`：

```bash
sudo -n sh -c 'command -v rxctl'
```

- 能找到 → 不动；
- 找不到 → 解析 sudo 的 `secure_path`，挑一个可写目录建 `/usr/bin/rxctl` 软链；
- 目标已存在（普通文件 / 指向别处）→ **不覆盖**，只提示。

`rxctl uninstall all --purge` 会删除**指向本二进制**的这条软链（二进制本身保留）。
手工兜底：

```bash
sudo ln -sf /usr/local/bin/rxctl /usr/bin/rxctl   # /usr/bin 在 sudo secure_path 里
```

**它会往哪里写东西**

| 路径 | 谁创建 | 说明 |
|---|---|---|
| `/usr/local/bin/rxctl` | 你 | 二进制（**建议固定这里**；`agent install` 会把绝对路径写进 unit） |
| `/usr/bin/rxctl` | `rxctl install`（必要时） | 指向 `/usr/local/bin/rxctl` 的软链，只为 `sudo rxctl` 方便；`uninstall all --purge` 会移除 |
| `/etc/reality-exit/` | `rxctl install tcp` | `meta.env` / `users.tsv` / `config.json` / `usage.tsv`（700 / 600） |
| `/etc/hy/` | `rxctl install udp` | 自签证书 + UDP 配置（700 / 600） |
| `/var/lib/rxctl/agent.json` | `rxctl agent` | 上次执行、错误、端口、自愈次数、最近用量（600） |
| `/etc/systemd/system/reality-exit.service`<br>`hy-exit.service` / `rxctl-agent.service` | rxctl | 由 `install` / `agent install` 生成 |
| `/etc/cron.d/rxctl-quota` | `rxctl quota cron` | 仅在不用 agent、走 cron 时存在 |

> ⚠️ **顺序**：先把二进制放到最终位置，再执行 `rxctl install` / `rxctl agent install`。
> 之后**不要随意挪动二进制**——agent 单元里的 `ExecStart` 是绝对路径；挪了要重新 `agent install`。
> 升级时覆盖 `/usr/local/bin/rxctl` 后，记得 `systemctl restart rxctl-agent`（运行中的进程还用旧 inode）。

### 1.2 两种部署方式（单机 / 中转）

> 先区分两个概念：
> **方案** = 协议（`tcp` = VLESS + Reality，`udp` = Hysteria2）；
> **部署方式** = 客户端怎么到出口机（单机直连 / 入口中转）。

#### 方式 A：单机直连

```text
客户端 ──TCP/UDP──> 出口机（rxctl）
```

- 出口机直接监听公网端口，客户端直连。
- 方案：**TCP 或 UDP 任选**；也可以 `install all` 同机并存，各自端口。
- 适合：客户端能直连出口，或只有一台机器。
- 注意：出口端口直接暴露，只开必要端口，UUID/口令要足够强。

#### 方式 B：入口中转

```text
客户端 ──> 入口机（DNAT + MASQUERADE）──> 出口机（rxctl）
```

- 入口机只做**四层转发**（不解析协议、不持有凭据）；出口机可以只信任入口机 IP。
- 方案：**推荐 TCP（VLESS + Reality）**。中转只做 TCP DNAT 最简单；
  UDP 也能转发，但需要入口机额外做 UDP DNAT，且链路上 UDP 更容易被限速/QoS。
- 适合：客户端到出口的国际段不稳定，需要国内入口做中转。
- 注意：入口机放行入口端口；出口机安全组只需放行“入口机 IP → 出口端口”。

| | 单机直连 | 入口中转 |
|---|---|---|
| 机器数 | 1 | 2 |
| 协议 | TCP / UDP | 以 TCP 为主 |
| 延迟 | 少一跳 | 多一跳 |
| 暴露面 | 出口直接暴露 | 出口只对入口开放 |
| 典型用途 | 海外 VPS 直接给客户端 | 国内入口 + 海外出口 |

> rxctl 只管理**出口机**；入口机的 DNAT/MASQUERADE 是普通 iptables 规则，不由 rxctl 接管。
> 出口机上的用户、端口、配额、计量，两种部署方式完全一致。

---

## 2. 命令速查

| 命令 | 作用 | 常用写法 |
|---|---|---|
| `install <tcp\|udp\|all>` | 安装方案 | `sudo rxctl install tcp --port 443 --user alice --quota 100G` |
| `status [tcp\|udp\|all]` | 服务、端口、用户数 | `sudo rxctl status` |
| `user add/del/list/show/rotate/enable/disable/port` | 多用户 + 独立端口 + 配额 | `sudo rxctl user add bob --port 20001 --quota 500G` |
| `links [--scheme] [--qr]` | 客户端链接 | `sudo rxctl links --qr` |
| `qr <链接/文本>` | 给任意链接打印二维码（如经中转机的链接） | `sudo rxctl qr "vless://..."` |
| export [--format ...] | 导出客户端配置（uri / clash / singbox） | sudo rxctl export --format clash > rxctl.yaml |
| `usage [--refresh] [--month]` | 用量与配额 | `sudo rxctl usage --refresh` |
| `quota check/set/cron/status` | 累计/熔断/月重置 | `sudo rxctl quota check` |
| `agent run/once/install/uninstall/status` | 常驻统计 + 自愈（替代 cron） | `sudo rxctl agent install` |
| `selftest [tcp\|udp\|all]` | 真实出网自检 | `sudo rxctl selftest` |
| `logs [tcp\|udp] [-f] [-n N]` | 日志 | `sudo rxctl logs tcp -f` |
| `port <tcp\|udp> <N>` | 改默认端口 | `sudo rxctl port tcp 8443` |
| `fw restore\|status` | 重建/查看计数链（开机用） | `sudo rxctl fw restore` |
| `start\|stop\|restart [tcp\|udp\|all]` | 服务控制 | `sudo rxctl restart tcp` |
| `doctor [--selftest]` | 只读体检 | `sudo rxctl doctor` |
| `uninstall <tcp\|udp\|all> [--purge]` | 卸载 | `sudo rxctl uninstall udp --purge` |
| `hy2 <sub>` | UDP 的别名入口 | `sudo rxctl hy2 install --port 24446` |
| `version` / `-V` | 版本与依赖 | `rxctl version`（不需要 sudo） |

---

## 3. 分命令说明

### 3.1 `install` —— 安装

```bash
sudo rxctl install <tcp|udp|all> [--port N] [--udp-port N] \
    [--sni HOST] [--user NAME] [--quota SIZE] \
    [--open-firewall] [--dry-run]
```

| 参数 | 适用 | 说明 |
|---|---|---|
| `--port` | tcp / udp | 端口（tcp 默认 443 / udp 默认 24446） |
| `--udp-port` | all | `install all` 时 UDP 用的端口（默认 24446） |
| `--sni` | 两者 | Reality 伪装站点 / 自签证书 CN（默认 `www.microsoft.com`） |
| `--user` | 两者 | 初始用户名（默认 `default`） |
| `--quota` | 两者 | 初始用户配额（`100G`、`500M`、纯数字=字节；省略=不计量） |
| `--open-firewall` | 两者 | 本机 firewalld/ufw 放行（云安全组仍需手动放行） |
| `--dry-run` | 两者 | 只打印计划，不落盘、不改服务 |

要点：

- **初始用户直接占用 `--port`**（带端口 → 可计量）。
- 安装会生成 Reality 密钥/自签证书、写 `config.json`、装 systemd 单元并 `enable --now`。
- 装 UDP 时 **云防火墙必须单独放行 UDP**（TCP 规则不覆盖 UDP）。
- 装前先看计划：`sudo rxctl install tcp --port 443 --dry-run`。

### 3.2 `status` —— 一眼看状态

```bash
sudo rxctl status [tcp|udp|all] [--json]
```

```text
方案      状态      服务      端口           用户
tcp      已安装     active   443,8443      2
udp      未安装     -        -              0
```

### 3.3 `user` —— 用户 / 端口 / 配额

```bash
sudo rxctl user list [--scheme tcp|udp|all] [--json]
sudo rxctl user add  <名字> [--scheme tcp|udp|both] [--port N] [--quota 500G] [--password P] [--obfs P]
sudo rxctl user del  <名字> [--scheme ...]
sudo rxctl user show <名字> [--scheme ...] [--json] [--qr]
sudo rxctl user rotate  <名字> [--scheme ...]   # 换凭据，旧链接立即失效
sudo rxctl user enable|disable <名字>            # 解封/封禁该用户端口
sudo rxctl user port <名字> <新端口> [--scheme tcp|udp]
```

| 规则 | 说明 |
|---|---|
| 独立端口 | 一人一端口才能按人计量；`--port` 同一方案内不能重复 |
| 不设端口 | 走默认端口、**不计量**（`user add` 不带 `--port`） |
| `--scheme both` | TCP 与 UDP 同名，但凭据/端口各自独立 |
| `del` | 会同时清计数规则、封禁规则、用量记录；**不允许删到最后一个用户** |
| `rotate` | 只换凭据，端口/配额不变 |

```bash
# 加一个走 20001 的用户，500G/月
sudo rxctl user add bob --port 20001 --quota 500G
# 取他的链接（手机扫码）
sudo rxctl user show bob --qr
# 他超量了，临时解封（用量不清零）
sudo rxctl user enable bob
```

### 3.4 `links` —— 客户端链接

```bash
sudo rxctl links [--scheme tcp|udp|all] [--json] [--qr]
```

```text
== TCP / VLESS + Reality ==
  [alice] 443
    vless://...@203.0.113.10:443?...#alice
  [bob] 8443
    vless://...@203.0.113.10:8443?...#bob
```

- `--qr`：每条链接后打印二维码（终端可直接扫）。
- 链接里含 UUID/口令，**等同密码**；状态目录 600，所以需要 `sudo`。
- 链接也可落盘：`sudo rxctl links > /root/links.txt && chmod 600 /root/links.txt`。

### 3.4.1 `qr` —— 给任意链接出二维码

```bash
sudo rxctl qr "vless://..."     # 适合经中转机、server 不是本机的链接
```

`links --qr` / `user show --qr` 只画本机生成的链接；这条命令可以画任何文本链接。

### 3.4.2 `export` —— 导出客户端配置

`links` 只给单节点 URI；`export` 能直接生成**客户端要的完整配置**。

```bash
rxctl export --format uri        # vless:// / hysteria2://（默认）
rxctl export --format clash      # 完整 Clash/Mihomo YAML（Clash Verge Rev 可直接导入）
rxctl export --format singbox    # 完整 sing-box 客户端 JSON（SFI/SFM/CLI）
```

| 参数 | 说明 |
|---|---|
| `--format` | `uri` / `clash` / `singbox`（默认 `uri`） |
| `--user` | 只导出某个用户（默认全部） |
| `--scheme` | `tcp` / `udp` / `all` |
| `--server` | 覆盖地址（经中转机时填中转机 IP） |
| `--port-map` | 端口映射，如 `24443=28544,24444=28545` |
| `--port` | 单节点时覆盖端口 |
| `--rules` | `cn`（默认，内置国内直连规则）/ `none` |

经中转机导出 Clash 配置：

```bash
sudo rxctl export --format clash --server 120.26.2.197 \
  --port-map 24443=28544,24444=28545 > /root/clash.yaml
```

Clash Verge Rev → Profiles → Import → Local File，选这个文件即可（规则已内置）。

---
### 3.5 `usage` / `quota` —— 用量与熔断
```bash
sudo rxctl usage [--refresh] [--month YYYY-MM] [--scheme ...]
sudo rxctl quota <check|set|cron|uncron|status>
```

```text
用户      方案    端口    已用     配额     占比     状态
alice    tcp    443     1.2G    100.0G   1.2%    正常
bob      tcp    8443    480M    50.0G    0.9%    正常
```

| 项 | 规则 |
|---|---|
| 口径 | **出方向**（服务器→客户端 = 用户下载） |
| 熔断 | 超配额只 `DROP` 该端口；不重启服务、不影响别人 |
| 重置 | 每月 1 号自动清零并解封 |
| 累计 | 按"上次内核计数 → 本次"的**增量**；计数被清零（重启/重建规则）也不会丢历史 |
| 并发 | `quota check` 带文件锁，cron 与 agent 不会同时跑 |

```bash
sudo rxctl quota set bob 800G     # 改配额（同时改 users.tsv 并重启服务）
sudo rxctl quota cron             # 装 /etc/cron.d/rxctl-quota（每 10 分钟）
sudo rxctl quota uncron           # 移除 cron
```

### 3.6 `agent` —— 常驻统计 + 自愈（推荐，替代 cron）

```bash
sudo rxctl agent install [--interval 10m]   # 写 rxctl-agent.service 并启动；自动删除 quota cron
sudo rxctl agent status [--json]            # 服务状态、上次执行、自动重启次数、最近用量
sudo rxctl agent once [--interval 10m]      # 只跑一轮（排障）
sudo rxctl agent uninstall                  # 停止并移除；如需回退：sudo rxctl quota cron
```

每轮做三件事：

1. `quota check`（累计/熔断/月重置）；
2. 检查 `reality-exit` / `hy-exit`，掉了就 `systemctl restart`（同一服务 60 秒内不重复重启）；
3. 状态落盘 `/var/lib/rxctl/agent.json`（600）：上次执行、耗时、错误、端口/用户、重启次数、最近用量。

> `agent` 与 `quota cron` **二选一**，`agent install` 会自动删 cron。

### 3.7 `selftest` —— 真实出网自检

```bash
sudo rxctl selftest [tcp|udp|all] [--json]
```

它会在本机临时起一个 sing-box 客户端（随机本地端口），用**用户自己的链接**去 `https://ip.sb`，
再和 `meta.env` 里的 `PUB_IP` 比对；跑完即删临时配置。

```text
tcp    OK    出口 203.0.113.10
udp    OK    未安装
```

失败时返回退出码 5，并提示去看 `journalctl -u reality-exit`。

### 3.8 `doctor` —— 只读体检

```bash
sudo rxctl doctor [--selftest] [--json]
```

```text
系统           OK    <发行版> · kernel <ver> · systemd <ver>
资源           OK    <内存> · swap <大小> · disk <剩余>
sing-box       OK    <版本> (/usr/local/bin/sing-box)
agent          OK    服务 active（上次 <时间>）
tcp            OK    服务 active
tcp 配置       OK    校验 OK · 权限 0600
tcp 端口       OK    443 listening
tcp 用户       OK    2 个用户
```

`--selftest` 会额外跑一次真实出网；有 FAIL 时退出码 5。

### 3.9 `logs` / `port` / `fw` / 服务控制

```bash
sudo rxctl logs tcp -f              # 跟踪 reality-exit 日志
sudo rxctl logs udp -n 100          # 最近 100 行
sudo rxctl port tcp 8443            # 改默认端口（走默认端口的用户一起迁移）
sudo rxctl fw restore               # 重建 RX_USAGE/RX_USAGE_IN（开机脚本调用；幂等）
sudo rxctl fw status                # 看计数链规则
sudo rxctl start|stop|restart [tcp|udp|all]
```

### 3.10 `hy2` —— UDP 的别名入口

```bash
sudo rxctl hy2 install --port 24445 --sni www.microsoft.com
sudo rxctl hy2 info          # 链接 + 二维码
sudo rxctl hy2 status|restart|logs|selftest
sudo rxctl hy2 user add phone --port 24450
```

等价于 `rxctl install udp …` / `rxctl user … --scheme udp`，只是更贴近 shell 版的使用习惯。

### 3.11 `uninstall`

```bash
sudo rxctl uninstall tcp            # 停服务、删单元、清计数/封禁规则（保留 /etc/reality-exit）
sudo rxctl uninstall udp --purge    # 连 /etc/hy（证书）一起删
sudo rxctl uninstall all --purge    # 两套全清；保留 sing-box 二进制（可能别人也在用）
```

### 3.12 `version` / `help` / `completion`

```bash
rxctl -V                  # 单行：rxctl 0.4.8 (linux/amd64)
rxctl version             # 构建信息 + sing-box 版本/路径
rxctl help user           # 子命令帮助
rxctl completion bash     # 补全脚本（bash/zsh/fish/powershell）
```

---

## 4. 典型场景

### 4.1 新机器从零上线（TCP）

```bash
sudo ./rxctl install tcp --port 443 --sni www.microsoft.com --user alice --quota 100G
sudo ./rxctl user add bob --port 8443 --quota 50G
sudo ./rxctl selftest
sudo ./rxctl links --qr
sudo ./rxctl agent install     # 二选一：agent（推荐）或 quota cron
```

云厂商控制台记得放行 **TCP 443/8443**（443 若只给中转机，就只放中转机 IP）。

### 4.2 加一个用户 / 给手机用

```bash
sudo rxctl user add alice --port 20001 --quota 300G     # TCP
sudo rxctl user show alice --qr                          # 导入手机

sudo rxctl hy2 user add phone --port 24450 --quota 100G  # UDP（若装了 udp）
```

### 4.3 限额与解封

```bash
sudo rxctl usage --refresh          # 先累计再展示
sudo rxctl quota set alice 500G     # 调额
sudo rxctl user disable alice       # 手动封（不影响别人）
sudo rxctl user enable alice        # 解封（用量不清零）
```

### 4.4 换端口（避开封锁）

```bash
sudo rxctl port tcp 8443            # 只影响走默认端口的用户
sudo rxctl selftest                 # 确认新端口可用
```

> 别忘同步云安全组，以及中转机上的 DNAT。

### 4.5 迁移 / 回滚（与 shell 版并存）

两版**读同一套状态文件**（`/etc/reality-exit/`、`/etc/hy/`），可随时互切：

```bash
sudo ./rxctl uninstall all          # 停 rxctl 的服务（保留文件）
# 或：把 shell 版重新指到同样的目录
```

本仓库只包含工具本身；生产环境的变更记录请自行留存。

### 4.6 客户端推荐（按链接类型选）

rxctl 生成两种链接，能用的客户端不一样：

- `vless://…` —— VLESS + Reality（TCP）：需要支持 **Reality** 的客户端；
- `hysteria2://…` —— Hysteria2（UDP/QUIC）：**Xray 内核不支持**，要用 sing-box / Mihomo 这类内核。

> **分流发生在客户端**：服务端只负责转发，不按域名判断；下面的“国内直连”配置都在客户端做。

> 以下为第三方客户端，与本项目无关联；版本更新快，**以各客户端最新版为准**。
> iOS 上的 Karing / sing-box / Shadowrocket 需要非中国区 Apple ID。

#### 各端首选与备选（稳定优先）

> 选型原则：**稳定 > 轻量 > 国内直连 > 更新频率**。
> “更新慢但极稳”可以接受（如 Shadowrocket）；**已停更且无独特优势**的不再推荐（NekoRay 停在 2024-12）。

| 平台 | 首选 | 备选（按推荐顺序） | 说明 |
|---|---|---|---|
| Windows | **Clash Verge Rev** | **v2rayN** · **FlClash** · sing-box（CLI） | Verge：Tauri 轻量、规则分流、两种协议、长期稳定；v2rayN 极稳 + 一键绕过大陆，但仅 VLESS；FlClash 更新快、Flutter、两种协议；CLI 最轻但配置型 |
| macOS | **Clash Verge Rev** | **FlClash** · **Karing** · sing-box（SFM） | Verge 通用 Intel/ARM；FlClash/Karing 更偏新手；SFM 官方最轻、配置型 |
| Linux（桌面） | **Clash Verge Rev** | **FlClash** · mihomo（CLI） | 桌面 GUI 优先；CLI 适合轻量场景 |
| Linux（服务器/路由） | **sing-box** | **mihomo** | 几 MB、最轻最稳；mihomo 适合复用 Clash 规则 |
| Android | **NekoBox for Android** | **v2rayNG** · **FlClash** · **Karing** | NekoBox：稳定、轻、两种协议、自带大陆分流预设（2026-02 仍更新）；v2rayNG 极稳 + 一键绕过大陆但仅 VLESS；FlClash/Karing 更新更快 |
| iOS | **Shadowrocket** | **Karing** · **sing-box（SFI）** | Shadowrocket 付费但稳定、轻、规则成熟；Karing 易用；SFI 官方免费最轻 |
| OpenWrt | **mihomo** | **sing-box** · passwall2（仅 VLESS） | 内核最轻；passwall2 稳定但 Xray 不支持 Hysteria2 |

> **为什么首选不总是“最新”**：FlClash/Karing 更新快，但 Clash Verge Rev 在体积（Tauri）、规则生态和稳定性上更均衡；
> Android 上 NekoBox 虽然 2026-02 才更新，但轻、稳、自带大陆预设，比“更新更快但更重”的 FlClash 更适合当首选。

#### 分流（国内直连）怎么开

目标：**国内域名/IP → 直连，其余 → 代理**；DNS 也要分流（国内域名用国内 DNS，其余走代理解析）。

| 客户端 | 快速做法 |
|---|---|
| Clash Verge Rev / FlClash / Karing / mihomo（Clash 兼容） | 用「规则」模式，profile 里加：`GEOSITE,cn,DIRECT` → `GEOIP,CN,DIRECT` → `MATCH,PROXY`；DNS 用 `nameserver-policy` 分流 |
| NekoBox for Android | 「路由」→ 选 **Bypass LAN and Mainland China** 预设 |
| v2rayN / v2rayNG | 「路由」→「**绕过大陆**」（内置 geosite:cn + geoip:cn 直连），其余走代理（仅 VLESS） |
| Shadowrocket | 「配置」模式，规则加 `DOMAIN-SUFFIX,cn,DIRECT` / `GEOIP,CN,DIRECT`，最后 `FINAL,PROXY` |
| sing-box（SFI/SFM/CLI） | `route.rules`：`rule_set: geosite-cn / geoip-cn → direct`，`final: proxy`；DNS 用 `rules` 分流 |

> ⚠️ **只导入一条 `vless://`/`hysteria2://` 链接时，客户端通常没有分流规则**——
> Clash Verge / FlClash / Karing 要加载一份带规则的 profile；NekoBox、v2rayN/v2rayNG 用自带预设最省事。
> 另外**优先按域名（geosite/rule-set）判断**，只靠 `GEOIP,CN` 容易把共享 CDN 误判成国内。

#### 完整兼容表（更多选择）

| 平台 | 客户端 | VLESS+Reality | Hysteria2 | 说明 |
|---|---|---|---|---|
| Windows | **Clash Verge Rev** | ✅ | ✅ | 首选；Mihomo / Tauri |
| Windows | **v2rayN** | ✅ | ⚠️ 需切 sing-box 内核 | 极稳；仅 VLESS 时首选 |
| Windows | **FlClash** | ✅ | ✅ | 备选；Flutter / Mihomo |
| Windows | **sing-box（CLI）** | ✅ | ✅ | 最轻，配置型 |
| Windows | NekoRay | ✅ | ✅ | **停更 2024-12**，不推荐 |
| Windows | Hiddify | ✅ | ✅ | Flutter，易用 |
| macOS | **Clash Verge Rev** | ✅ | ✅ | 首选 |
| macOS | **FlClash** | ✅ | ✅ | 备选 |
| macOS | **Karing** | ✅ | ✅ | 备选，易用 |
| macOS | sing-box（SFM） | ✅ | ✅ | 官方，最轻 |
| Linux | **Clash Verge Rev** | ✅ | ✅ | 桌面首选 |
| Linux | **FlClash（AppImage）** | ✅ | ✅ | 备选 |
| Linux | **sing-box（CLI）** | ✅ | ✅ | 服务器/路由首选，最轻 |
| Linux | mihomo（CLI） | ✅ | ✅ | Clash 规则复用 |
| Linux | NekoRay | ✅ | ✅ | **停更 2024-12** |
| Android | **NekoBox for Android** | ✅ | ✅ | 首选；自带大陆分流预设 |
| Android | **v2rayNG** | ✅ | ❌ | 极稳；仅 VLESS |
| Android | **FlClash** | ✅ | ✅ | 备选 |
| Android | **Karing** | ✅ | ✅ | 备选，易用 |
| Android | Hiddify | ✅ | ✅ | Flutter |
| iOS | **Shadowrocket** | ✅ | ✅ | 首选；付费 |
| iOS | **Karing** | ✅ | ✅ | 备选 |
| iOS | **sing-box（SFI）** | ✅ | ✅ | 官方、免费、最轻 |
| OpenWrt | **mihomo** | ✅ | ✅ | 首选 |
| OpenWrt | **sing-box** | ✅ | ✅ | 备选 |
| OpenWrt | passwall2 | ✅ | ❌ | Xray；仅 VLESS |

**导入方式**

1. **复制链接**：`sudo rxctl links` → 客户端「从剪贴板导入」/「Import from clipboard」；
2. **扫码**：`sudo rxctl links --qr`，或 `sudo rxctl user show <用户名> --qr`；
3. **sing-box 配置片段**：`sudo rxctl user show <用户名> --json`（可直接粘到 outbounds）。

> 同一链接在多台设备导入，**共用该端口的配额**（计量按端口）。要给不同设备分开计量，
> 就 `user add` 各建一个用户/端口。

---

## 5. 常见问题

| 现象 | 原因 / 处理 |
|---|---|
| `rxctl links` 说"需要 root" | 状态目录 700；用 `sudo rxctl links` |
| `sudo rxctl` 报 `command not found` | 部分发行版（如 CentOS/RHEL）的 sudo `secure_path` 不含 `/usr/local/bin`；用 `sudo /usr/local/bin/rxctl`，或建 `/usr/bin/rxctl` 软链 |
| `links` 真的显示"未安装" | 看 `sudo rxctl status`；确认没带错 `--root`；确认 `/etc/reality-exit/meta.env` 存在 |
| UDP 客户端一直超时 | **云安全组没放行 UDP**（TCP 规则不覆盖 UDP） |
| `usage` 数字不涨 | 先 `sudo rxctl usage --refresh`（或等 agent 10 分钟一轮）；确实不涨再看是否没有独立端口（无端口=不计量）、计数链是否丢了（`sudo rxctl fw restore`） |
| 端口冲突 | 同一方案内一人一端口；换 `--port` 或先 `user port` |
| 月重置 | 每月 1 号由 `quota check`（agent/cron）自动清零并解封 |
| 配置改坏了 | `sing-box check` 会拦住；回滚到备份的 `users.tsv`/`meta.env` 后 `sudo rxctl restart tcp` |

---

## 6. 退出码

| 码 | 含义 |
|---|---|
| 0 | 成功 |
| 1 | 一般错误（无数据、用户不存在） |
| 2 | 参数错误（含端口冲突） |
| 3 | 前置条件不满足（非 root、状态目录读不到、功能属于后续里程碑） |
| 4 | 外部命令失败（systemctl / iptables / sing-box） |
| 5 | 校验失败（配置非法、自检未通过） |

---

## 7. 文件与数据

| 文件 | 内容 | 权限 |
|---|---|---|
| `/etc/reality-exit/meta.env` | `PUB_IP PORT SNI PRIV PUB SID` | 600 |
| `/etc/reality-exit/users.tsv` | `name<TAB>uuid<TAB>port<TAB>quota` | 600 |
| `/etc/reality-exit/usage.tsv` | `name port quota month out in last_out last_in blocked` | 600 |
| `/etc/hy/meta.env` | `HY_SNI HY_IP HY_PORT HY_CERT HY_KEY` | 600 |
| `/etc/hy/users.tsv` | `name<TAB>password<TAB>port<TAB>quota<TAB>obfs` | 600 |
| `/var/lib/rxctl/agent.json` | agent 状态（上次执行/错误/端口/重启次数/最近用量） | 600 |
| systemd | `reality-exit.service` / `hy-exit.service` / `rxctl-agent.service` | 644 |

---

## 8. 构建与开发

```bash
make build            # 本机 → bin/rxctl
make cross            # Linux amd64/arm64、macOS arm64、Windows amd64 → dist/
make test             # go test ./...
make vet              # go vet ./...
make release          # Linux amd64/arm64/armv7 → dist/ + SHA256SUMS
```

或直接：

```bash
CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=0.4.8" -o rxctl ./cmd/rxctl
```

安装到 `$GOPATH/bin`：

```bash
go install github.com/allong6/rxctl/cmd/rxctl@latest
```

### 8.1 发布（自动）

推一个 tag 即可，GitHub Actions（`.github/workflows/release.yml`）会自动：
测试 → 构建 linux amd64/arm64/armv7 → 生成 `SHA256SUMS` → 创建 Release。

```bash
git tag -a v0.4.8 -m "rxctl v0.4.8"
git push origin v0.4.8
```

也可以在本地先验证产物：`make release`（只构建，不发布）。

目录结构：

```text
cmd/rxctl/          # 命令入口（main / m1 / agent）
internal/engine/    # 安装、配置生成、防火墙计数、配额、自检、端口
internal/state/     # meta.env / users.tsv / usage.tsv 读写（原子写）
internal/links/     # vless:// / hysteria2:// 链接生成
internal/check/     # 只读探测（systemctl / ss / sing-box）
internal/qr/        # 内置二维码（vendored skip2/go-qrcode，MIT）
internal/ui/        # 表格 / JSON 输出
docs/               # CLI.md（完整规格）、DESIGN.md（设计与里程碑）
```

构建机需要 Go 1.23+；**目标机什么都不需要**（静态二进制，不依赖 bash/curl/jq/qrencode）。
完整 CLI 规格见 [docs/CLI.md](docs/CLI.md)，设计与里程碑见 [docs/DESIGN.md](docs/DESIGN.md)。

## 9. 许可

MIT，见 [LICENSE](LICENSE)。`internal/qr` 内嵌 [skip2/go-qrcode](https://github.com/skip2/go-qrcode)（MIT），保留其 LICENSE。
