# vLLM Monitor

直连 vLLM `/metrics` 的轻量实验看板。Go + React/TypeScript + Vite + Tailwind/shadcn 风格基础组件 + ECharts + TanStack Query + Zustand + SQLite。

## 运行

本仓库保存源码。请先按下方“开发”步骤构建，或使用 Docker 构建运行。已有可执行文件时，Windows 运行 `vllm-monitor.exe`；Linux/WSL：

```bash
chmod +x vllm-monitor
./vllm-monitor
```

浏览器打开 http://127.0.0.1:9090 。默认 SQLite 文件为当前目录 `monitor.db`。配置和 Bearer Token 存在本地数据库，请保护该文件。

```bash
./vllm-monitor -listen 0.0.0.0:9090 -db /path/monitor.db
```

程序没有多用户登录，仅用于可信本地或实验内网。默认只绑定回环地址。远端使用 SSH 端口转发，避免公开到互联网。

Docker：`docker compose up -d --build`。在 Docker 中，`localhost` 指容器本身，访问宿主机服务请使用 `host.docker.internal` 或实际服务器 IP。服务必须监听容器可达地址。默认宿主端口仍仅暴露在 127.0.0.1。

## 使用

- 点击侧栏加号添加 `http://GPU服务器:8000`，可配置 Bearer Token 和 metrics 路径。
- 扫描指定主机的端口范围，发现后点击添加。扫描仅请求 metrics，不发送推理请求。
- 单层服务列表，模型自动识别；每个端点独立维护观测会话。缺少启动时间也可以记录历史。
- 首次连接、恢复连接、身份线索变化和每 30 秒查询 `/v1/models`；比较模型 ID/root，忽略 created、权限和列表顺序。查询失败保留已有信息。
- 1 秒默认采样，10 秒吞吐窗口，60 秒延迟窗口，内存保留 30 分钟。首次需要两个有效样本才能显示速率。
- 打开“记录”保存 30 秒聚合窗口，默认保留 7 天；关闭记录会刷新当前有效窗口。不保存开启记录之前的内存数据。
- 会话选择器查看历史，原始指标查看全部已解析 Gauge/Counter/Histogram 序列。
- 模型 ID/root 变化、有效进程启动时间变化、连续离线至少 30 秒后恢复时开启新会话。短暂离线只保留缺口；Counter 回退只重置速率，不切分会话。点击“新会话”会在下一次成功采样时开始记录新的会话。

## 开发

要求 Go 1.24+、Node.js 22+。

```bash
cd web
npm ci
npm run build
npm test
cd ..
go test ./...
go build -buildvcs=false -o vllm-monitor .
```

必须先构建前端，Go embed 才能找到 web/dist。前端开发时后台运行 Go 服务，再 `cd web && npm run dev`，Vite 将 /api 转发至 9090。

Windows 构建（在有 Go 的环境）：`GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -buildvcs=false -o vllm-monitor.exe .`。SQLite 为纯 Go 驱动。

## 验证

Go 测试覆盖启动时间解析、Counter 回退/采集缺口、会话切分、旧数据库迁移和 Histogram 解析；Vitest 覆盖前端缓冲区去重、过期和缺失值。

集成测试需要干净数据库，按以下顺序启动：

```bash
python3 scripts/mock_vllm.py
./vllm-monitor -db test.db
python3 scripts/integration.py
```

模拟器只用于验证，真实服务不注入任何模拟数据。集成测试使用不提供启动时间的模拟端点，验证实时速率、元数据查询间隔、同端口换模型切分、旧会话模型不变、历史刷新、扫描和记录开关。

## 当前实现边界

当前版本为 v0.3。现有面板覆盖吞吐、Running/Waiting、完成速率、缓存、TTFT/ITL/TPOT 均值、TTFT P95 与 MTP 总体接受情况。后端也保存 Histogram bucket 增量和 Gauge 极值，支持后续扩展。

尚未完成：逐个 vLLM 版本的全部日志指标覆盖验收、MTP 各位置图、独立 Engine 明细、自动周期扫描、主题切换、可配置保留期、完整元数据清理、历史导出。扩展指标目前可在原始指标中查看；不声称全部日志已由专用图表覆盖。历史曲线当前显示窗口均值/速率，Gauge 极值仍保存在数据库。P95 为 bucket 插值估计，落入无限 bucket 时返回缺失。

面向直连单个服务端点；负载均衡后端交替返回多个实例时，无法可靠切分各实例。观测会话只代表可观察的连续区间，不保证与进程生命周期一一对应。窗口聚合以有效采样间隔归入窗口，约 30 秒，不保证整点边界。实际断连与进程重启只能在下一次采样时观察到。

未连接用户真实 GPU 服务验证；Docker 配置已提供，镜像需在目标环境构建验证。

## Web 与桌面启动

Web 模式继续使用上面的 Go 程序或 Docker，桌面模式采用 Electron 窗口，复用相同的 React 静态页面和 Go 采集器，不需要另启 Web 服务。

```bash
# 构建前端
cd web
npm ci
npm run build
cd ..

# Web 模式
go build -buildvcs=false -o vllm-monitor .
./vllm-monitor

# 或桌面模式（另一个终端，从仓库根目录）
cd desktop
npm ci
npm start
```

桌面启动脚本会编译当前平台的 Go 后端，需要 Go 在 PATH 中，或设置 GO_BINARY。正式安装包内嵌后端，用户不需要安装 Go/Node.js。窗口默认 1440×960，字体及控件仍按 100% 桌面缩放呈现。

桌面端仅监听 127.0.0.1 随机可用端口。默认数据库在 Electron 的用户数据目录（Windows 通常是 `%APPDATA%/vllm-dashboard-lite-desktop/monitor.db`）；需要和 Web 模式使用同一个数据库时，可设置绝对路径环境变量 `VLLM_DASHBOARD_DB`，并在两种模式之间先退出原进程，不要同时采集同一个数据库。关闭窗口会向后端发送退出指令，刷新聚合窗口后退出；父进程异常退出导致 stdin 关闭时，后端也会退出。

桌面打包在目标系统执行：

```bash
cd desktop
npm run dist
```

Windows 输出 NSIS 安装包，Linux 输出 AppImage，macOS 输出 DMG，文件在 `desktop/release/`。构建脚本按当前系统/CPU 编译 Go 后端，暂不支持从另一平台交叉打包桌面安装器。安装包尚未配置签名。`npm run pack` 只生成未封装的应用目录。

## 进程启动时间排查

`process_start_time_seconds` 是 Prometheus Python 客户端的进程采集指标，**不是 vLLM 保证提供的业务指标**。vLLM 的 registry 实现检测到 `PROMETHEUS_MULTIPROC_DIR` 时，会创建只注册 MultiProcessCollector 的 registry；这一路径可能不包含标准 ProcessCollector。不能只凭单模型、TP=1 或单端口就判断该字段必然存在。不同版本及启动路径需以实际 `/metrics` 为准。当前 CLI 的 API Server 数量默认可能跟随 DP 数量，一个端口并不意味着只有一个 API Server。

直接在能访问 vLLM 的机器检查：

```bash
curl -fsS http://HOST:PORT/metrics | grep -E '^(# TYPE process_start_time_seconds|process_start_time_seconds)'
curl -fsS http://HOST:PORT/version
```

若启用了认证，在 curl 请求中使用服务要求的认证 Header。没有任何匹配行意味着上游未暴露；不是 Dashboard 丢失了已经存在的值。不要为此擅自删除 vLLM 内部多进程环境变量，也不要用 `/v1/models.created` 或首次连接时间假冒启动时间。

Dashboard 支持 Gauge 及省略 TYPE 的数值字段，并区分未暴露、无效值、多个不同启动时间。字段缺失、无效或多值冲突时，进程启动时间显示“未提供”，仍可保存观测会话。曾观测到的有效启动时间保留于当前会话，短暂缺失不会切分。

参考：[vLLM registry 源码](https://docs.vllm.ai/en/latest/api/vllm/v1/metrics/prometheus/)、[进程指标说明](https://docs.vllm.ai/en/stable/design/metrics/)。

## v0.2 修改

- 服务列表按端点 URL、ID 固定排序，不受 map 遍历、状态变化或 SSE 刷新影响。
- 增加独立桌面启动器、随机回环端口、用户数据目录、单实例窗口和后端退出管理。
- 更准确地报告启动时间缺失原因，增加无 TYPE 指标解析和相应测试。

桌面验证：`cd desktop && npm test` 检查启动协议；`npm run prepare:backend && node smoke.cjs` 检查实际 Go 子进程。当前开发环境禁止 Electron 单实例锁使用的本地 socket，原生窗口和安装器需要在目标 Windows/Linux 桌面上验证。

## v0.3 观测会话

- 会话信息分别显示“观测开始”和“进程启动”；不使用 `/v1/models.created` 推断启动时间。
- 监控程序重新启动后，首次成功采样创建新会话，旧历史保留。新会话使用独立数据库 ID，因此相同端口、相同模型、相同启动时间也能手动分开记录。
- 旧数据库自动迁移，保留原 run ID 与关联聚合数据；数据库升级后不建议用旧版程序继续写入。
- 模型/root 变化最迟在下一次元数据查询时发现，发现时切分；查询间隔内发生的精确切换时刻无法追溯。
- 同名、同路径模型快速重启，且上游没有提供启动标识时，可能无法自动发现。需要严格实验边界时点击“新会话”。
- 历史接口继续使用 `/api/runs` 和 `/api/history?run=...`；新会话接口为 `POST /api/endpoints/{id}/sessions`，返回 202 表示已排队。

## 手动打包 CI

打开 GitHub **Actions → Package → Run workflow**：

| 选项 | 输出 |
| --- | --- |
| desktop + windows（默认） | x64 桌面目录 ZIP，解压后运行 `vLLM Dashboard Lite.exe` |
| desktop + linux | x64 桌面目录 tar.gz，解压后运行 `vllm-dashboard-lite-desktop` |
| web + windows | 内嵌页面的 x64 `vllm-monitor.exe` ZIP |
| web + linux | 内嵌页面的 x64 `vllm-monitor` tar.gz |

完成后在该次运行的 **Artifacts** 下载；同时提供 SHA-256 校验文件。桌面包必须整体解压，不能只复制 EXE；Linux 桌面仍需系统提供图形环境及 Electron 的运行库。桌面包未签名。macOS 安装包仍可按上面的本地打包方式构建，CI 暂不覆盖。

节省构建时间与存储的设置：

- 仅 `workflow_dispatch`，提交、PR、Tag 均不会自动启动；每次只运行一个目标，无平台矩阵。
- 仅使用标准 Windows/Linux runner；仓库变成私有时直接跳过构建，避免意外消耗私有仓库额度。
- 缓存 npm、Go、Electron 下载；前端与后端各编译一次，Web 模式不安装 Electron。
- 输出应用目录压缩包，省去安装器生成、签名工具下载；压缩等级 1，上传时不二次压缩。
- 同分支同目标重复运行会取消旧任务；每次最多 20 分钟；产物仅保留 **1 天**，请及时下载。

GitHub 当前规定公开仓库使用标准 runner 的计算免费；Artifact 存储与缓存仍受账户配额/计费设置约束。该工作流不修改账单或缓存容量设置，也不自动发布 Release。参考：[GitHub Actions 计费](https://docs.github.com/en/billing/concepts/product-billing/github-actions)。
