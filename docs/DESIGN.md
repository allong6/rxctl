# rxctl · 开发计划与设计

> 目标：把现在的 shell 工具（`reality-exit.sh`，1056 行）重写为**单一 Go 二进制**，
> 覆盖 **安装 → 用户/端口/配额 → 用量 → 自检 → 卸载** 全流程，并为后续配置中心预留 agent。
>
> 状态：**M1 + M2 已完成**（v0.4.7，2026-10-27 生产上线）　最后更新：2026-10-27

---

## 1. 目标与非目标

| 目标 | 说明 |
|---|---|
| **单二进制** | 交叉编译 `CGO_ENABLED=0`，拷到任意 Linux 直接跑，无运行时依赖 |
| **无外部依赖** | 不依赖 bash/curl/jq/awk/qrencode/包管理器；sing-box 可选内嵌 |
| **幂等** | 任何命令重复执行结果一致；安装可重入 |
| **可审计** | `--dry-run` 只打印将执行的动作；`--print-config` 只输出生成的配置 |
| **可测试** | 配额计算、用户解析、配置生成均为纯函数 + 单测 + golden 文件 |
| **可交接** | 状态文件保持现有格式，可与 shell 版互切 |

| 非目标 | 原因 |
|---|---|
| 重写分流/协议/透明代理 | sing-box 已在做 |
| 重写一次性排查脚本（`trace-*` / `probe-*` / `investigate-*`，约 6000 行） | 用完即弃 |
| 引入数据库 / 常驻 API 服务（除 M2 的 agent 外） | 单机场景不需要 |

---

## 2. 功能清单

**两套方案对等，都要有**：TCP（VLESS + Reality）与 UDP（Hysteria2）。可只装一套，也可同时装；两套各自独立端口、独立服务、互不影响。

### 2.1 两套共用

| 命令 | 作用 | 阶段 |
|---|---|---|
| `rxctl doctor` | 只读体检：OS/内核/端口/防火墙/服务/配置/线路 | M0 |
| `rxctl usage` | 用量报表（读 `users.tsv` + iptables 计数，TCP 与 UDP 都统计） | M0 |
| `rxctl links` | 列出全部客户端链接（TCP 每用户 + UDP 每用户） | M0 |
| `rxctl uninstall [--purge]` | 卸载并清理（服务、单元、配置、计数链、cron） | M1 |

### 2.2 TCP 方案（VLESS + Reality）

| 命令 | 作用 | 阶段 |
|---|---|---|
| `rxctl install` | 安装 TCP 方案（Reality，免证书） | M1 |
| `rxctl show <user> [--json]` | 单用户链接 / sing-box outbound 片段 | M1 |
| `rxctl port <new>` | 修改默认端口 | M1 |

### 2.3 UDP 方案（Hysteria2）

| 命令 | 作用 | 阶段 |
|---|---|---|
| `rxctl hy2 install` | 安装 UDP 方案（自签证书、口令、可选 obfs） | **M1** |
| `rxctl hy2 info` | 链接 + sing-box 片段 + 二维码 | **M1** |
| `rxctl hy2 status / restart / logs` | 状态与日志 | **M1** |
| `rxctl hy2 user add/del` | **每个 UDP 用户独立端口**（可计量/限配额） | M2 |

### 2.4 用户与配额（两套通用）

| 命令 | 作用 | 阶段 |
|---|---|---|
| `rxctl user add/del/list` | 用户 + 独立端口 + 配额（TCP） | M1 |
| `rxctl quota check` | 累计用量、超限封端口、跨月重置 | M1 |
| `rxctl quota cron` / `uncron` | 安装 / 移除定时任务 | M1 |
| `rxctl agent` | 常驻：定时统计、上报、自愈（替代 cron） | M2 |
| `rxctl center pull/push` · `upgrade` | 配置中心、自更新 | M3 |

## 3. 目录结构

```
rxctl/
  go.mod
  cmd/rxctl/main.go            # 命令注册与参数解析
  internal/config/             # sing-box 配置：结构体 + 生成 + 校验
  internal/users/              # users.tsv / usage.tsv 读写（原子写、flock）
  internal/quota/              # 累计 / 熔断 / 月重置（纯函数，重点单测）
  internal/netfw/              # iptables 计数链（幂等）+ 防火墙探测（firewalld/ufw）
  internal/installer/          # sing-box 获取（内嵌或下载+校验）、systemd 单元、swap
  internal/hy2/                # hysteria2 入站（证书、口令、obfs）
  internal/agent/              # 常驻循环
  internal/report/             # 表格/JSON 输出、二维码
  testdata/                    # golden 文件（配置样例、用量样例）
```

**依赖原则**：只用标准库 + `spf13/cobra`（命令）+ `golang.org/x/crypto`（X25519）；其余自实现，减少供应链风险。

---

## 4. 状态与数据（兼容现有文件）

沿用现有路径，保证与 shell 版**双向可切**：

| 文件 | 内容 | 权限 |
|---|---|---|
| `/etc/reality-exit/meta.env` | `PUB_IP PORT SNI PRIV PUB SID`（key=value） | 600 |
| `/etc/reality-exit/users.tsv` | `name<TAB>uuid<TAB>port<TAB>quota_bytes` | 600 |
| `/etc/reality-exit/usage.tsv` | `name port quota month out in last_out last_in blocked` | 600 |
| `/etc/reality-exit/config.json` | 由上面生成，**不手改** | 600 |
| `/etc/hy/meta.env` | `HY_PORT HY_SNI HY_PASS HY_OBFS HY_IP` | 600 |

**写入规则**：`tmp 文件 → fsync → rename`；全程 `flock` 防并发；任何失败不留下半写文件。

---

## 5. 安装流程（`rxctl install`）

```
① preflight    root / 发行版 / 内核 / 端口占用 / 磁盘 / 内存 / 是否已装
② plan         生成动作清单；--dry-run 打印后退出（0 副作用）
③ binary       内嵌解压或下载 → sha256 校验 → /usr/local/bin/sing-box
④ keys         X25519 密钥对 → 写 meta.env(600)
⑤ config       结构体 → json.Marshal(缩进) → sing-box check → 落盘(600)
⑥ systemd      写单元 → daemon-reload → enable --now
⑦ firewall     探测 firewalld/ufw 放行；云控制台给明确提示
⑧ selftest     起临时客户端 → 请求 ip.sb → 与本机公网 IP 比对
⑨ report       打印链接 + 二维码 + 下一步提示
⑩ rollback     任一步失败：按已做步骤逆序回滚（停服务→删单元→删配置→保留他人共用的二进制）
```

**每步要求**：幂等（已存在且正确则跳过）、可回滚、失败即停并说明原因。

**UDP 方案（`rxctl hy2 install`）的差异**

| 步骤 | TCP | UDP |
|---|---|---|
| ③ binary | sing-box | 同一个 binary（`hysteria2` 是它的入站类型） |
| ④ keys | X25519（Reality） | **自签证书 + 口令**（可选 `obfs` 混淆口令） |
| ⑤ config | 单个/每用户 inbound | `hysteria2` 入站；**多用户 = 多入站（每用户独立端口）** |
| ⑦ firewall | 放行 **TCP** 端口 | **必须单独放行 UDP 端口**（TCP 规则不覆盖） |
| ⑧ selftest | 请求 `ip.sb` 比对出口 IP | 同左（走本机 UDP 回环） |

---

## 6. 用户 / 端口 / 配额流程

```
rxctl user add bob --port 20001 --quota 500G
   ├─ 写 users.tsv（name / uuid / port / quota_bytes）
   ├─ 重建 config：按端口分组，一人一个 inbound
   ├─ 重启服务
   └─ 建计数规则：RX_USAGE(--sport 20001) / RX_USAGE_IN(--dport 20001)

rxctl usage
   ├─ 先跑一次 quota check（累计增量）
   └─ 输出：用户 / 端口 / 已用 / 配额 / 占比 / 状态

rxctl quota check   （由 cron 或 agent 每 10 分钟调用）
   ├─ 读计数 → 累加进 usage.tsv
   ├─ 用量 ≥ 配额 → DROP 该端口 + 记 blocked=1
   └─ 月份变化 → 清零 + 解封
```

**状态机**

```
        ┌────────── 月重置 ──────────┐
        ▼                            │
   正常 ──用量≥配额──► 已封 ──────────┘
   （正常收流）      （该端口 DROP，其他人不受影响）
```

| 规则 | 说明 |
|---|---|
| 计量口径 | **出方向**（服务器→客户端，即用户下载） |
| 前提 | **必须一人一端口**；共用端口无法按人计量 |
| 熔断粒度 | 单端口 DROP，不重启服务、不影响其他用户 |
| 重置 | 每月 1 号自动清零并解封 |
| **UDP 同样适用** | Hysteria2 的一个入站只对应一个口令；**每用户一个 UDP 端口 = 每用户一个 `hysteria2` 入站**，计量链按 `--sport/--dport <该端口>` 即可，与 TCP 同一套逻辑 |
| 不设端口的用户 | 走默认端口、**不计量**（`user add` 不带 `--port` 即可） |

---

## 7. 验证矩阵（安装到验证全流程）

| # | 阶段 | 验证项 | 方式 | 期望 |
|---|---|---|---|---|
| 1 | 装前 | 端口未被占用 | `rxctl doctor` | 报“可用” |
| 2 | 装后 | 服务运行 | `systemctl is-active reality-exit` | `active` |
| 3 | 装后 | 端口监听 | `ss -tlnp` | 有监听 |
| 4 | 装后 | 配置合法 | `sing-box check -c …` | 退出码 0 |
| 5 | 装后 | **本机自检** | `rxctl doctor --selftest` | 出口 IP = 本机公网 IP |
| 6 | 装后 | **外部可用** | 客户端导入链接 → 打开 `ip.sb` | 显示服务器 IP |
| 7 | 用户 | 加人 | `user add` + `links` | 链接可导入、可上网 |
| 8 | 计量 | 下载 10 MB | `usage` | 数字增长且口径正确 |
| 9 | 熔断 | 配额设小值后超限 | `usage` | 状态“已封”，`INPUT` 有 `DROP` |
| 10 | 熔断 | 封后访问 | 客户端请求 | 失败（该用户） |
| 11 | 隔离 | 其他用户 | 正常访问 | 不受影响 |
| 12 | 重置 | 模拟跨月 | `quota check` | 清零并解封 |
| 13 | 卸载 | `uninstall --purge` | 逐项核对 | 服务/单元/配置/计数链/cron 全清，他人的 sing-box 二进制保留 |
| **14** | UDP 装后 | UDP 端口监听 | `ss -lunp` | 有监听 |
| **15** | UDP 装后 | **云防火墙放行 UDP** | 境外机器测该端口 | 可达（漏放行时境外也不可达） |
| **16** | UDP 装后 | UDP 自检 | `rxctl hy2 selftest` | 出口 IP = 本机公网 IP |
| **17** | UDP 装后 | 外部可用 | 客户端导入 `hysteria2://` | 正常上网 |
| **18** | UDP 计量 | 下载 10 MB | `rxctl usage` | UDP 用户数字增长 |
| **19** | 两套并存 | 同时装 | `install` + `hy2 install` | 两个服务各自 active、互不影响 |

---

## 8. 退出码与输出约定

| 码 | 含义 |
|---|---|
| 0 | 成功 |
| 1 | 一般错误 |
| 2 | 参数错误 |
| 3 | 前置条件不满足（非 root、端口被占、发行版不支持） |
| 4 | 外部命令失败（systemctl / iptables） |
| 5 | 校验失败（配置、自检） |

- 默认人类可读表格；`--json` 输出机器可读（便于将来接监控）。
- `--verbose` 打印每条外部命令；`--dry-run` 只打印不执行。

---

## 9. 测试策略

| 层次 | 内容 |
|---|---|
| 单测 | 配额累计（跨月、计数归零、口径）、`users.tsv` 解析（2 列旧格式 + 4 列新格式）、配置生成 golden |
| 集成 | 用 `--root /tmp/rxctl-test` 覆盖根目录 + `--dry-run` 跑全流程，不碰本机系统 |
| 端到端 | 在非生产 VM 上执行验证矩阵 1–13 |

---

## 10. 里程碑

| 里程碑 | 内容 | 产出 | 状态 |
|---|---|---|---|
| **M0** | 骨架 + `doctor` + `usage` + `links`（**纯只读**） | 可跑二进制，零生产风险 | ✅ 2026-10-27 |
| **M1** | `install`(TCP/UDP) + `user` + `quota` + `show` + `port` + `fw` + `selftest` + `uninstall` | **两套方案齐备**，可替换核心 shell | ✅ **v0.3.2，已在生产环境切换** |
| **M2** | `agent`（常驻统计/自愈，替代 cron） | 去掉 cron，具备自愈 | ✅ **v0.4.0** |
| **M3** | 配置中心对接 + 自更新 | 可远程调整策略 | ⏳ 未开始 |

**切换判据**：M1 完成且验证矩阵 1–13 全通过 → 已满足（1–19 均实测，包含 TCP/UDP、两套并存、单机/中转）。

**M1 相对计划的三处补充**：

| 补充 | 原因 |
|---|---|
| `rxctl fw restore` | 有些机器的 iptables 没有持久化服务，重启后计数链会丢；开机脚本里调用它即可恢复（幂等、不清零已有计数） |
| `selftest` 真出网 | 按计划实现：本机起临时 sing-box 客户端（随机本地端口）→ 请求 `https://ip.sb` → 与 `PUB_IP` 比对，跑完即删 |
| 内置二维码 | `internal/qr` 内嵌 MIT 版 `skip2/go-qrcode`，保持"单二进制、无外部命令依赖"（不依赖 `qrencode`） |

**M2 实现说明（`agent`）**

| 项 | 做法 |
|---|---|
| 常驻 | `rxctl-agent.service`（`Restart=always`），`ExecStart=rxctl agent run --interval 10m` |
| 定时统计 | 每轮调用 `QuotaCheck()`：累计、超限封端口、跨月清零；**带文件锁**（`/tmp/rxctl-quota.lock`），cron 与 agent 不会同时跑 |
| 自愈 | 每轮检查 `reality-exit` / `hy-exit` 是否 active，掉了就 `systemctl restart`；同一服务 60 秒内不重复重启（防风暴） |
| 上报 | 先落盘 `/var/lib/rxctl/agent.json`（600）：上次执行、耗时、错误、各方案状态/端口/用户、自动重启次数、最近用量；M3 接配置中心时直接改这里 |
| 替代 cron | `rxctl agent install` 自动删除 `/etc/cron.d/rxctl-quota`；`agent uninstall` 后可用 `rxctl quota cron` 回退 |
| 运维 | `rxctl agent status [--json]`；`rxctl doctor` 会显示 agent 状态 |

---

## 11. 迁移与并存

| 阶段 | 做法 |
|---|---|
| M0 | 只读命令，直接在生产机跑（不写任何文件） |
| M1 | 与 shell 版**写同样的路径**，可随时切回 |
| 切换后 | shell 版保留在 `scripts/`，文档双份；出现问题时 `reality-exit.sh` 仍可接管 |

---

## 12. 风险与对策

| 风险 | 影响 | 对策 |
|---|---|---|
| 重写引入回归 | 生产断网 | 只读先行、并存、验证矩阵、可逆步骤 |
| 二进制需被信任 | 审计困难 | 开源代码、`--print-config`、构建可复现 |
| agent 权限过大 | 安全面 | 最小权限：只操作本项目自有链与服务 |
| 内嵌 sing-box 增大体积 | 分发 | 默认下载 + 校验；`--embed` 构建标签可选 |
| 团队不熟 Go | 维护 | 单文件命令结构 + 文档 + 现有 shell 兜底 |

---

## 13. 待定（需要确认）

| # | 问题 | 建议 |
|---|---|---|
| 1 | 项目名 | `rxctl`（relay/exit control） |
| 2 | sing-box 是否内嵌 | **默认下载 + sha256 校验**；提供 `-tags embed` 可内嵌 |
| 3 | 是否现在开工 | 建议先做 **M0**（只读，1 天，零风险） |
| 4 | 配置中心形态 | 自建 HTTP + JSON（简单、可控），先定义 schema |
