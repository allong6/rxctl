# rxctl · CLI 规格（完整）

## 0. 方案总览（本工具的"能做什么"，放在文档而非 `version` 里）

| 方案 | 协议 | 实现 | 凭据 | 证书 | 端口 |
|---|---|---|---|---|---|
| **tcp** | VLESS + Reality（`xtls-rprx-vision`） | **sing-box · `vless` 入站** | UUID | **不需要** | TCP |
| **udp** | Hysteria2（QUIC） | **sing-box · `hysteria2` 入站** | 口令（+可选 obfs `salamander`） | 自签 | **UDP** |
| 共同 | — | 两种入站可同机并存 | 每人一凭据 | — | 每人可选独立端口 |

| 能力 | 状态 |
|---|---|
| 多用户（TCP 每用户 UUID；UDP 每用户口令） | ✓ |
| 每用户独立端口 | ✓ |
| 按端口计量（出方向） | ✓ |
| 配额熔断（超限封端口，不重启、不影响他人） | ✓ |
| 月度自动重置 | ✓ |
| 二维码（内置生成） | ✓ |
| 定时任务 `quota cron` | ✓ |
| 常驻 `agent`（替代 cron） | ✓ |
| 配置中心 `center pull/push` | M3 |
| TUIC / VLESS over QUIC（sing-box 支持，未启用） | ○ |

> `rxctl version` 只报版本与依赖；**"支持什么、怎么实现"以本节文档为准**，`version --json` 里以紧凑字段供程序读取。



> 供评审：这是**完整命令面**，不是分阶段范围。M0/M1/M2 只是实现顺序，**命令与参数一次定好**。
>
> 原则：**两套方案（TCP / UDP）× 多用户 × 独立端口 × 独立配额，全部支持。**
> 多用户「可以不用，但不能没有」——单用户时不带 `--port` 即回落到共享默认端口。

---

## 1. 命令树

```
rxctl
├─ help      [<command>]          -h / --help    显示帮助（任意命令后加 -h 同理）
├─ version                        -V / --version 版本
├─ doctor            [--selftest] [--json]
├─ install   <tcp|udp|all>   [--port N] [--udp-port N] [--sni HOST] [--user NAME] [--quota 500G] [--open-firewall] [--dry-run]
├─ status    [<tcp|udp|all>] [--json]
├─ start | stop | restart  [<tcp|udp|all>]
├─ selftest  [<tcp|udp|all>] [--json]          # 真实出网（本机临时客户端）
├─ logs      [<tcp|udp|all>] [-f] [-n N]
├─ user
│   ├─ list    [--scheme tcp|udp|all] [--json]
│   ├─ add     <name> [--scheme tcp|udp|both] [--port N] [--quota 500G] [--password P] [--obfs P]
│   ├─ del     <name> [--scheme ...]
│   ├─ show    <name> [--scheme ...] [--json] [--qr]
│   ├─ rotate  <name> [--scheme ...]
│   ├─ enable  <name> [--scheme ...]
│   ├─ disable <name> [--scheme ...]
│   └─ port    <name> <N> [--scheme tcp|udp]
├─ links     [--scheme ...] [--json] [--qr]
├─ export    [--format uri|clash|singbox] [--user N] [--scheme ...]
│             [--server H] [--port N] [--port-map 24443=28544] [--rules cn|none]
├─ usage     [--month YYYY-MM] [--scheme ...] [--refresh] [--json]
├─ quota
│   ├─ check   [--json]
│   ├─ set     <name> <size> [--scheme ...]
│   ├─ cron | uncron
│   └─ status  [--json]
├─ port      <tcp|udp> <N>
├─ fw        restore | status                  # 重建/查看计数链（开机脚本调用）
├─ uninstall <tcp|udp|all> [--purge]
├─ hy2
│   ├─ install [--port N] [--sni HOST] [--user NAME] [--quota 500G] [--dry-run]
│   ├─ info | status | restart | logs | selftest
│   └─ user add|del|list ...                   # 等价于 user ... --scheme udp
├─ agent     run|once|install|uninstall|status [--interval 10m]
├─ center    pull | push [--server URL]        （M3）
└─ completion bash|zsh|fish|powershell
```

短参数只有 7 个：`-h` `-V` `-v` `-y` `-p` `-f` `-s`（见 §11.2）；命令别名只有 4 个：`log` `users` `link` `test`（见 §11.3）。

`<tcp|udp|all>` 省略时的默认值：
`status / start / stop / restart / selftest / logs` → **all**；
`install / uninstall` → **必须显式给**（避免误装/误删）。

---

## 2. 全局参数

| 参数 | 说明 |
|---|---|
| `--root DIR` | 替换根目录（测试用，如 `--root /tmp/rxctl-test`） |
| `--verbose, -v` | 打印每条外部命令（systemctl / iptables / sing-box） |
| `--json` | 机器可读输出（便于接监控） |
| `--dry-run` | 只打印将执行的动作，不落任何文件、不改任何服务 |
| `--yes, -y` | 跳过交互确认 |
| `--timeout 30s` | 外部命令超时 |
| `-h, --help` | 显示帮助（任何命令都支持） |
| `-V, --version` | **单行**版本（`rxctl 0.1.0 linux/amd64 go1.23.12`），便于脚本解析 |
| `version`（子命令） | **详细**版本：构建信息 + **支持的代理方案与实现** + 内置能力 + 本机安装状态 |

---

## 3. 只读命令

### 3.1 `doctor`

```
rxctl doctor [--selftest] [--json]
```

| 检查项 | 说明 |
|---|---|
| 系统 | 发行版、内核、systemd、磁盘、内存/swap |
| 端口 | 各 scheme 的端口是否被占用、是否在监听 |
| 防火墙 | 本机 firewalld/ufw 状态；是否存在以 `DROP` 结尾的自定义链 |
| 服务 | 各 scheme 的 systemd 单元状态 |
| 配置 | `sing-box check` 结果、文件权限是否为 600 |
| 用户 | `users.tsv` 解析是否正常、有无重复端口 |
| 线路 | 出口 IP、到本机的连通性（`--selftest` 才跑真实出网） |

**示例**

```bash
$ rxctl doctor
系统      Debian 13 / kernel 6.12 / systemd 257 / mem 2.0G / swap 2.0G    OK
TCP       服务 active · 端口 443 listening · 配置校验 OK · 权限 600       OK
UDP       服务 active · 端口 24446/udp listening · 配置校验 OK           OK
防火墙    firewalld inactive · ufw inactive · 无 DROP 自定义链           OK
用户      3 个（tcp 2 / udp 1），无重复端口                              OK
出口      8.x.x.x（本机公网 IP 一致）                                    OK
```

### 3.2 `status`

```bash
$ rxctl status --json
{
  "tcp": {"service":"active","ports":[443],"users":2},
  "udp": {"service":"active","ports":[24446,24447],"users":2}
}
```

### 3.3 `links`

```bash
$ rxctl links
== TCP / VLESS + Reality ==
  [alice] 443   vless://…
  [bob]   20001 vless://…
== UDP / Hysteria2 ==
  [alice] 24446 hysteria2://…
  [bob]   20002 hysteria2://…
```
`--qr` 会为每条链接打印二维码（单条用 `user show --qr`）。

### 3.4 `usage`

```bash
$ rxctl usage
用户     方案  端口    已用     配额     占比     状态
alice    tcp   443     1.2G     none     -        正常
bob      tcp   20001   480G     500G     96.0%    正常
alice    udp   24446   12G      100G     12.0%    正常
bob      udp   20002   100G     100G     100.0%   已封
```
`--refresh` 先跑一次累计再输出；`--month 2026-09` 查历史月份。

---

### 3.4.1 `export` —— 客户端配置导出

```bash
rxctl export --format uri        # 单节点 URI（等同 links）
rxctl export --format clash      # 完整 Clash/Mihomo YAML
rxctl export --format singbox    # 完整 sing-box 客户端 JSON
```

| 参数 | 说明 |
|---|---|
| `--user` | 只导出某个用户 |
| `--scheme` | tcp / udp / all |
| `--server` | 覆盖服务器地址（中转场景） |
| `--port-map` | 端口映射（原端口=新端口） |
| `--rules cn/none` | 是否内置国内直连规则（默认 cn） |

`clash` 输出含 proxies / proxy-groups / rules / dns，可直接导入 Clash Verge Rev。
### 3.5 `version`

```bash
rxctl version [--json]
```

**只报版本、构建与依赖**，不铺开能力清单（能力见 §0 文档，本机状态见 `status`）。

| 写法 | 输出 |
|---|---|
| `rxctl -V` / `--version` | 单行：`rxctl 0.1.0 (linux/amd64)`（给脚本解析） |
| `rxctl version` | 5 行：版本、commit/build、os/arch、**sing-box 版本与路径** |
| `rxctl version --json` | 结构化（含 `schemes` 与 `capabilities` 紧凑字段，供监控/支持） |

**示例**

```bash
$ rxctl version
rxctl 0.1.0
  commit  : 3f9a2c1   build : 2026-10-27
  os/arch : linux/amd64
  sing-box: 1.14.3  (/usr/local/bin/sing-box)
```

`--json`：

```json
{
  "version": "0.1.0",
  "commit": "3f9a2c1",
  "build": "2026-10-27",
  "os": "linux",
  "arch": "amd64",
  "deps": {"sing_box": "1.14.3", "sing_box_path": "/usr/local/bin/sing-box"},
  "schemes": ["tcp", "udp"],
  "capabilities": {"per_user_port": true, "metering": true, "quota_block": true,
                   "monthly_reset": true, "qr": true, "tuic": false, "vless_quic": false}
}
```

**分工：想知道什么，用哪个命令**

| 想知道 | 用 |
|---|---|
| 版本号（脚本） | `rxctl -V` |
| 版本 + 构建 + sing-box 依赖 | `rxctl version` |
| 装了哪些方案、服务状态、端口、用户数 | `rxctl status` |
| 支持哪些方案、怎么实现、有哪些能力 | 文档 §0（本节不展开） |
| 机器体检 / 排障 | `rxctl doctor` |
| 命令与参数 | `rxctl help` |

> ⚠️ **权限**：`/etc/reality-exit`、`/etc/hy` 是 `700 root`，**除 `version/help` 外所有命令都要 `sudo`**。
> 非 root 跑只读命令会返回**退出码 3**并提示"需要 root"，**不会**误报"未安装"。

---

## 4. 安装 / 运行控制

```bash
rxctl install tcp  --port 443   --sni www.microsoft.com
rxctl install udp  --port 24446 --sni example.com
rxctl install all  --port 443   --sni www.microsoft.com     # UDP 端口取 --udp-port，缺省 24446
```

| 参数 | 适用 | 说明 |
|---|---|---|
| `--port` | tcp / udp | 端口（tcp 默认 443 / udp 默认 24446）**install udp 也用它** |
| `--udp-port` | all | `install all` 时 UDP 用的端口（缺省 24446） |
| `--sni` | 两者 | Reality 伪装站点（tcp）/ 自签证书 CN（udp） |
| `--user` | 两者 | 初始用户名（缺省 `default`） |
| `--quota` | 两者 | 初始用户配额（如 `1.5T`；省略 = 不计量） |
| `--open-firewall` | 两者 | 本机防火墙放行（tcp 放 TCP，udp 放 UDP） |
| `--dry-run` | 两者 | 只打印安装计划 |

> 初始用户**直接占用 `--port`**（带端口 → 可计量）；之后 `user add --port` 加的人各自独立入站，
> 不带 `--port` 的用户走默认端口、**不计量**。

```bash
rxctl start all / stop all / restart udp
rxctl selftest all          # 各方案跑一次真实出网
rxctl logs tcp -f
```

---

## 5. 多用户管理（两套都支持）

### 5.1 用户矩阵

| 操作 | TCP | UDP |
|---|---|---|
| 独立端口 | ✓ `--port` | ✓ `--port`（**每人一个 hysteria2 入站**） |
| 独立凭据 | UUID | 口令（可选 obfs 口令） |
| 独立配额 | ✓ | ✓（按该用户端口计量） |
| 轮换凭据 | `rotate` → 新 UUID | `rotate` → 新口令 |
| 封禁/解封 | `disable` / `enable`（DROP 该端口，不删用户） | 同 |
| 删除 | `del` | `del` |
| 不设端口 | 走默认端口、不计量 | 同 |

### 5.2 常用命令

```bash
# 加一个 TCP 用户，独立端口 + 500G 配额
rxctl user add bob --scheme tcp --port 20001 --quota 500G

# 加一个 UDP 用户（独立端口 + 口令，可加混淆）
rxctl user add carol --scheme udp --port 20002 --quota 100G --obfs <口令>

# 两套同时建（同名用户，TCP 与 UDP 各一份凭据）
rxctl user add dave --scheme both --port 20003 --quota 200G

# 列出 / 查看 / 取链接
rxctl user list
rxctl user show bob            # 链接 + 二维码
rxctl user show bob --json     # sing-box outbound 片段

# 换凭据（旧链接立即失效，客户端要重新导入）
rxctl user rotate bob

# 临时封禁（不删用户，用量保留）
rxctl user disable bob
rxctl user enable bob

# 改端口（会重建配置并重启对应服务）
rxctl user port bob 20011
```

### 5.3 行为说明

| 场景 | 行为 |
|---|---|
| `--port` 与其他用户/默认端口冲突 | 直接报错退出（码 2），不落盘 |
| `--quota` 省略 | 不计量（`none`） |
| `--scheme both` | TCP 与 UDP 用**同一个名字**，但凭据与端口独立 |
| `del` | 立刻失效；同时清掉计数规则、封禁规则与用量记录 |
| `rotate` | 只换凭据，端口与配额不变 |

---

## 6. 配额

```bash
rxctl quota check                 # 累计用量、超限封端口、跨月重置（cron/agent 调用）
rxctl quota set bob 500G          # 改配额（--scheme 区分）
rxctl quota cron                  # 装 /etc/cron.d/rxctl-quota（每 10 分钟）
rxctl quota uncron
rxctl quota status                # 当前月份、各用户是否超额
```

| 项 | 规则 |
|---|---|
| 计量口径 | **出方向**（服务器→客户端，即用户下载） |
| 前提 | 该用户**有独立端口**；无端口的用户不计量 |
| 熔断 | 超配额 → `DROP` 该端口；不重启、不影响他人 |
| 重置 | 每月 1 号清零并解封 |
| 累计 | 按"上次内核计数 → 本次"的**增量**累加；计数被清零（重启/规则重建）时按新一段继续，**不丢历史** |
| 手动解封 | `rxctl user enable <name>`（用量不清零，再超会再封） |

> **持久化**：计数链 `RX_USAGE` / `RX_USAGE_IN` 不会自动随开机恢复。开机脚本里执行一次
> `rxctl fw restore` 即可（幂等，**只在缺规则时补**，不清零已有计数）。

### 6.5 `agent`：常驻统计 + 自愈（M2，替代 cron）

```bash
sudo rxctl agent install --interval 10m   # 写 rxctl-agent.service 并启动；自动移除 quota cron
rxctl agent status                        # 服务状态、上次执行、各方案、自动重启次数
sudo rxctl agent once                     # 只跑一轮（排障）
sudo rxctl agent uninstall                # 停用；如需回退：rxctl quota cron
```

| 项 | 规则 |
|---|---|
| 统计 | 每轮跑一次 `quota check`（带文件锁，和 cron 互斥） |
| 自愈 | 服务 inactive → `systemctl restart`；同一服务 60 秒内不重复重启 |
| 状态/上报 | `/var/lib/rxctl/agent.json`（600）：上次执行、耗时、错误、端口/用户、重启次数、最近用量 |
| 与 cron 的关系 | 二选一；`agent install` 会删掉 `/etc/cron.d/rxctl-quota`，`agent uninstall` 后可恢复 cron |

---

## 7. 端口、防火墙与卸载

```bash
rxctl port tcp 8443          # 改默认端口（走默认端口的用户一起迁移）
rxctl port udp 24450
rxctl fw restore             # 重建计数链（开机脚本调用；幂等）
rxctl fw status              # 查看 RX_USAGE / RX_USAGE_IN
rxctl uninstall tcp          # 卸载 TCP 方案
rxctl uninstall udp --purge  # 卸载 UDP 方案并删证书
rxctl uninstall all --purge  # 两套全清（保留他人共用的 sing-box 二进制）
```

---

## 8. 状态文件（两套都支持多用户）

| 方案 | 文件 | 内容 | 权限 |
|---|---|---|---|
| TCP | `/etc/reality-exit/meta.env` | `PUB_IP PORT SNI PRIV PUB SID` | 600 |
| TCP | `/etc/reality-exit/users.tsv` | `name<TAB>uuid<TAB>port<TAB>quota_bytes` | 600 |
| TCP | `/etc/reality-exit/usage.tsv` | `name port quota month out in last_out last_in blocked` | 600 |
| UDP | `/etc/hy/meta.env` | `HY_SNI HY_IP HY_PORT HY_CERT HY_KEY` | 600 |
| UDP | `/etc/hy/users.tsv` | `name<TAB>password<TAB>port<TAB>quota_bytes<TAB>obfs` | 600 |
| UDP | `/etc/hy/usage.tsv` | 同 TCP 结构 | 600 |
| 两者 | systemd | `reality-exit.service`（TCP）/ `hy-exit.service`（UDP） | 644 |

**兼容**：现有 shell 版 UDP 是"单口令"（`/etc/hy/meta.env` 里的 `HY_PASS`）→ 首次运行 `rxctl` 时自动迁移为 `users.tsv` 里的 `default` 用户，端口取原 `HY_PORT`。

---

## 9. 退出码

| 码 | 含义 |
|---|---|
| 0 | 成功 |
| 1 | 一般错误 |
| 2 | 参数错误（含端口冲突、用户不存在） |
| 3 | 前置条件不满足（非 root、端口被占、发行版不支持） |
| 4 | 外部命令失败（systemctl / iptables / sing-box） |
| 5 | 校验失败（配置非法、自检未通过，会打印具体原因） |

---

## 10. 与现有 shell 版的对应

| 现在的 shell | rxctl |
|---|---|
| `reality-exit.sh install` | `rxctl install tcp` |
| `reality-exit.sh hy2 install` | `rxctl install udp` |
| `reality-exit.sh user add X --port --quota` | `rxctl user add X --scheme tcp --port --quota` |
| `reality-exit.sh links` | `rxctl links` |
| `reality-exit.sh usage` | `rxctl usage` |
| `reality-exit.sh quota cron` | `rxctl quota cron` |
| `reality-exit.sh show X --json` | `rxctl user show X --json` |
| `reality-exit.sh hy2 info` | `rxctl user show X --scheme udp` 或 `rxctl links --scheme udp --qr` |
| `reality-exit.sh uninstall --purge` | `rxctl uninstall tcp --purge` |

---

## 11. 帮助与短参数

### 11.1 帮助

```bash
rxctl                  # 不带参数 = 总帮助
rxctl -h               # 同上
rxctl help             # 同上
rxctl help user        # 看 user 及子命令
rxctl user add -h      # 任何命令后加 -h 都行
```

每个命令的 `-h` 打印**用法 / 参数 / 一个示例**（由命令定义生成，不会与文档脱节）。

**总帮助的输出样例**（方案只占两行，不铺表格）：

```bash
$ rxctl
rxctl — 代理出口服务器管理

用法:
  rxctl <command> [subcommand] [flags]

常用命令:
  install <tcp|udp|all>    安装方案（tcp = VLESS+Reality，udp = Hysteria2）
  status                   服务、端口、用户数
  user add|list|del        用户管理（可分配独立端口与配额）
  links                    列出客户端链接
  usage                    用量与配额
  quota cron               配额熔断定时任务
  doctor                   体检
  help [command]           帮助（任意命令后加 -h 亦可）

支持方案:
  tcp   VLESS + Reality    sing-box vless 入站
  udp   Hysteria2 (QUIC)   sing-box hysteria2 入站
本机安装情况见: rxctl status
```

### 11.2 短参数（只保留有行业惯例或高频的）

| 短 | 长 | 说明 |
|---|---|---|
| `-h` | `--help` | 帮助 |
| `-V` | `--version` | 版本（小写 `-v` 是 verbose，两者不冲突） |
| `-v` | `--verbose` | 打印每条外部命令 |
| `-y` | `--yes` | 跳过交互确认（同 `apt -y`） |
| `-p` | `--port` | 端口（同 `docker -p` / `ssh -p`） |
| `-f` | `--follow` | 跟踪日志（同 `tail -f`） |
| `-s` | `--scheme` | `tcp` / `udp` / `all`（本工具高频修饰，故保留） |

**其余一律只写长参数**，避免字母被滥用：

| 长参数 | 为什么不给短参数 |
|---|---|
| `--dry-run` | `-n` 在别的工具里含义不一（`make -n` / `rsync -n` 是预演，但 `tail -n` 是行数）；本工具里高频度不高 |
| `--json` | `-j` 在多数工具里表示 jobs，容易误导 |
| `--quota` / `--sni` / `--obfs` / `--password` | 低频、仅创建时使用一次 |
| `--tail` | 与 `-n` 的历史含义冲突，只用长参数 |
| `--root` / `--timeout` | 调试用 |

### 11.3 命令别名（只保留 4 个）

| 命令 | 别名 | 为什么保留 |
|---|---|---|
| `help` | （无别名，直接用 `-h` / `--help`） | `-h` 已是惯例 |
| `logs` | `log` | 单复数习惯差异，几乎零成本 |
| `user` | `users` | 同上 |
| `links` | `link` | 同上 |
| `selftest` | `test` | 高频且短 |
| `version` | （无别名，直接用 `-V` / `--version`） | `-V` 已是惯例 |

**其余命令不加别名**，理由：

| 曾被考虑 | 不做的理由 |
|---|---|
| `i` / `in` → `install` | 动词缩写各国习惯不一；有 Tab 补全时收益接近零 |
| `st` / `ps` → `status` | `st` 非通用；`ps` 会让人联想到 `ps` 命令 |
| `up` / `down` → `start` / `stop` | `down` 在 compose 里含"删除"，语义会漂移 |
| `u` / `q` / `p` / `lk` / `usg` → 各命令 | 单字母极易与短参数混淆（`-p` 已是端口） |
| `un` / `remove` → `uninstall` | `rm`/`remove` 语义过重，误操作代价高 |
| `doc` / `diag` → `doctor` | 无惯例，且 `doctor` 本身好记 |
| 子命令别名（`ls`/`rm`/`new`…） | `user`、`quota` 之下层级浅，多一套别名反而增加记忆负担 |

### 11.4 不支持的写法

| 写法 | 处理 |
|---|---|
| 命令名前缀缩写（`rxctl inst tcp`） | **不支持**，直接报 `unknown command`（避免歧义与意外匹配） |
| 单字母别名与短参数混用（如 `-p` 既想表示 port 又想表示 password） | 已按上表固定，冲突的字母**不用** |

### 11.5 补全

```bash
rxctl completion bash > /etc/bash_completion.d/rxctl
rxctl completion zsh  > ~/.zsh/completions/_rxctl
```
有补全后，长命令名不需要别名；这也是上面大量别名被砍掉的原因。

### 11.6 常用写法（不加别名的版本）

| 目的 | 写法 |
|---|---|
| 看帮助 | `rxctl help user add` 或 `rxctl user add -h` |
| 预演装 TCP | `rxctl install tcp --dry-run` |
| 装两套 | `rxctl install all` |
| 看状态 | `rxctl status` |
| 看全部链接 | `rxctl links` |
| 加 TCP 用户 | `rxctl user add bob --scheme tcp --port 20001 --quota 500G` |
| 加 UDP 用户 | `rxctl user add carol --scheme udp --port 20002 --quota 100G` |
| 换凭据 | `rxctl user rotate bob` |
| 临时封禁 | `rxctl user disable bob` |
| 看用量 | `rxctl usage` |
| 跑一次配额检查 | `rxctl quota check` |
| 卸载 UDP | `rxctl uninstall udp --purge` |
