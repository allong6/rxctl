# rxctl

代理出口服务器管理工具：**一个静态二进制**（Go，无运行时依赖）管完
**安装 → 用户/端口/配额 → 链接/二维码 → 自检 → 自愈 → 卸载**。

支持两套方案，可只装一套，也可同机并存：

| 方案 | 协议 | 实现 | 凭据 | 证书 | 端口 |
|---|---|---|---|---|---|
| `tcp` | VLESS + Reality | sing-box `vless` 入站 | UUID | 不需要 | TCP |
| `udp` | Hysteria2（QUIC） | sing-box `hysteria2` 入站 | 口令（可选 obfs） | 自签 | UDP |

> 状态：**M1 + M2 完成**（v0.4.6）。M3（配置中心/自更新）未开始，见 [docs/DESIGN.md](docs/DESIGN.md)。

---

## 1. 30 秒上手

```bash
# 装 TCP：24443 给 office（1.5T/月），再给 mobile 开 24444（500G/月）
sudo rxctl install tcp --port 24443 --sni www.microsoft.com --user office --quota 1.5T
sudo rxctl user add mobile --port 24444 --quota 500G

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

**方式 C：`go install`（需要仓库是 public）**

```bash
go install github.com/allong6/rxctl/cmd/rxctl@latest    # → $GOPATH/bin/rxctl
sudo install -m 0755 "$(go env GOPATH)/bin/rxctl" /usr/local/bin/rxctl
```

**方式 D：从 GitHub Release 下载**（仓库 private，需先 `gh auth login`）

```bash
gh release download v0.4.6 -R allong6/rxctl \
  -p 'rxctl-linux-amd64' -p 'SHA256SUMS'
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

---

## 2. 命令速查

| 命令 | 作用 | 常用写法 |
|---|---|---|
| `install <tcp\|udp\|all>` | 安装方案 | `sudo rxctl install tcp --port 24443 --user office --quota 1.5T` |
| `status [tcp\|udp\|all]` | 服务、端口、用户数 | `sudo rxctl status` |
| `user add/del/list/show/rotate/enable/disable/port` | 多用户 + 独立端口 + 配额 | `sudo rxctl user add bob --port 20001 --quota 500G` |
| `links [--scheme] [--qr]` | 客户端链接 | `sudo rxctl links --qr` |
| `qr <链接/文本>` | 给任意链接打印二维码（如经 A 中转的链接） | `sudo rxctl qr "vless://..."` |
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
| `hy2 <sub>` | UDP 的别名入口 | `sudo rxctl hy2 install --port 24445` |
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
| `--quota` | 两者 | 初始用户配额（`1.5T`、`500G`、纯数字=字节；省略=不计量） |
| `--open-firewall` | 两者 | 本机 firewalld/ufw 放行（云安全组仍需手动放行） |
| `--dry-run` | 两者 | 只打印计划，不落盘、不改服务 |

要点：

- **初始用户直接占用 `--port`**（带端口 → 可计量）。
- 安装会生成 Reality 密钥/自签证书、写 `config.json`、装 systemd 单元并 `enable --now`。
- 装 UDP 时 **云防火墙必须单独放行 UDP**（TCP 规则不覆盖 UDP）。
- 装前先看计划：`sudo rxctl install tcp --port 24443 --dry-run`。

### 3.2 `status` —— 一眼看状态

```bash
sudo rxctl status [tcp|udp|all] [--json]
```

```text
方案      状态      服务      端口           用户
tcp      已安装     active   24443,24444    2
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
# 加一个走 20001 的同事，500G/月
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
  [office] 24443
    vless://...@8.219.54.143:24443?...#office
  [mobile] 24444
    vless://...@8.219.54.143:24444?...#mobile
```

- `--qr`：每条链接后打印二维码（终端可直接扫）。
- 链接里含 UUID/口令，**等同密码**；状态目录 600，所以需要 `sudo`。
- 链接也可落盘：`sudo rxctl links > /root/links.txt && chmod 600 /root/links.txt`。

### 3.4.1 `qr` —— 给任意链接出二维码

```bash
sudo rxctl qr "vless://..."     # 适合经 A 中转、server 不是本机的链接
```

`links --qr` / `user show --qr` 只画本机生成的链接；这条命令可以画任何文本链接。

### 3.5 `usage` / `quota` —— 用量与熔断
```bash
sudo rxctl usage [--refresh] [--month YYYY-MM] [--scheme ...]
sudo rxctl quota <check|set|cron|uncron|status>
```

```text
用户      方案    端口    已用     配额     占比     状态
mobile   tcp    24444   988.7M  500.0G   0.2%    正常
office   tcp    24443   6.5G    1.5T     0.4%    正常
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
tcp    OK    出口 8.219.54.143
udp    OK    未安装
```

失败时返回退出码 5，并提示去看 `journalctl -u reality-exit`。

### 3.8 `doctor` —— 只读体检

```bash
sudo rxctl doctor [--selftest] [--json]
```

```text
系统           OK    CentOS Linux 8 · kernel 4.18 · systemd 239
资源           OK    1723M total, 909M avail · swap 0M · disk 26G free
sing-box       OK    1.14.3 (/usr/local/bin/sing-box)
agent          OK    服务 active（上次 2026-10-27T15:44:54+08:00）
tcp            OK    服务 active
tcp 配置       OK    校验 OK · 权限 0600
tcp 端口       OK    24443 listening
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
sudo rxctl hy2 user add phone --port 24446
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
rxctl -V                  # 单行：rxctl 0.4.6 (linux/amd64)
rxctl version             # 构建信息 + sing-box 版本/路径
rxctl help user           # 子命令帮助
rxctl completion bash     # 补全脚本（bash/zsh/fish/powershell）
```

---

## 4. 典型场景

### 4.1 新机器从零上线（TCP）

```bash
sudo ./rxctl install tcp --port 24443 --sni www.microsoft.com --user office --quota 1.5T
sudo ./rxctl user add mobile --port 24444 --quota 500G
sudo ./rxctl selftest
sudo ./rxctl links --qr
sudo ./rxctl agent install     # 二选一：agent（推荐）或 quota cron
```

云厂商控制台记得放行 **TCP 24443/24444**（24443 若只给中转机，就只放中转机 IP）。

### 4.2 加一个同事 / 给手机用

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

生产环境的切换与回滚记录属于内部运维文档，不随本仓库发布。

---

## 5. 常见问题

| 现象 | 原因 / 处理 |
|---|---|
| `rxctl links` 说"需要 root" | 状态目录 700；用 `sudo rxctl links` |
| `sudo rxctl` 报 `command not found` | CentOS sudo `secure_path` 不含 `/usr/local/bin`；用 `sudo /usr/local/bin/rxctl`，或建 `/usr/bin/rxctl` 软链 |
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
CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=0.4.6" -o rxctl ./cmd/rxctl
```

安装到 `$GOPATH/bin`（仓库 public 时）：

```bash
go install github.com/allong6/rxctl/cmd/rxctl@latest
```

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
