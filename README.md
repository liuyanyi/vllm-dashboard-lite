# vLLM Monitor

直连 vLLM / TEI `/metrics` 的轻量推理监控看板，支持 Generation、Embedding、Reranker。Go + React/TypeScript + Vite + Tailwind/shadcn 风格基础组件 + ECharts + TanStack Query + Zustand + SQLite。

## 运行（YAML-only）

复制 `config.example.yaml` 为 `monitor.yaml`，根据实际服务修改。配置中只声明需要采集的端点，不需要 Prometheus，也不需要模型服务支持自动发现。

```bash
cp config.example.yaml monitor.yaml
./vllm-monitor -config monitor.yaml -db monitor.db
# 或直接运行（默认读取当前目录 monitor.yaml）
./vllm-monitor
# Docker：先创建 monitor.yaml，再启动
docker compose up -d --build
```

浏览器打开 http://127.0.0.1:9090。Windows 可执行文件同样支持 `-config`。也可设置环境变量 `VLLM_DASHBOARD_CONFIG`（推荐绝对路径）；显式 CLI 参数优先。缺少配置文件或字段无效时启动失败。使用 `endpoints: []` 可启动空总览。修改 YAML 后重启采集器生效。

```yaml
endpoints:
  - id: qwen-embed
    name: Qwen3 Embedding
    backend: tei
    task: embedding
    model: Qwen/Qwen3-Embedding-0.6B
    url: http://host.docker.internal:8001
    interval: 2000
    record: true
  - id: qwen-rerank
    backend: vllm
    task: reranker
    model: Qwen/Qwen3-Reranker-0.6B
    url: http://host.docker.internal:8002
    record: true
```

- 每个端点需要唯一且稳定的 `id`、`backend`（vllm / tei）、`task`（generation / embedding / reranker）、`model` 和 `url`；`name` 是可选显示名称。模型名与显示名可以重复。端点 URL + metrics_path 不得重复，避免同一地址重复采集。
- `metrics_path` 默认 /metrics；`interval` 为毫秒（默认 1000，允许 500–10000）；`record` 决定是否记录历史（默认 false）；可选 `token` 用于 Bearer 鉴权。请保护含 Token 的 YAML。
- Docker 默认以只读方式挂载 monitor.yaml，宿主服务可使用 host.docker.internal 或服务器 IP；localhost 指容器自身。指标端口不同（例如 TEI --prometheus-port）时直接填写指标 URL。默认只在宿主回环端口提供 UI。
- 程序没有多用户登录，仅用于可信本地或实验内网。不要公开到互联网。

## 使用

首页总览按 Generation 与 Embedding / Reranker 分组展示实例、状态、吞吐、队列与 P95，可点击进入原有详细视图。侧栏按 YAML 顺序显示，ID 区分同名实例。总览吞吐汇总标记有数据的实例数量，不平均各实例 P95，不混合不同单位的请求/样本数。未连接、离线、缺失或陈旧数据显示缺失。

- YAML 是唯一配置来源。已删除端口扫描、模型发现、/v1/models 元数据查询、模型 fingerprint、Web 增删修改端点 API 与 UI。原始指标标签仍保留用于统计，不用来确定实例身份。
- ID 关联历史记录；修改显示名/模型名无需避让，重启监控程序会按新配置创建新观测会话，旧会话的模型名不变。若更换 backend/task 或将 ID 用于无关服务，请使用新 ID，避免旧指标混用。
- 1 秒默认采样，10 秒吞吐窗口，60 秒延迟窗口，内存保留 30 分钟。首次需要两个有效样本才能显示速率。
- record: true 保存约 30 秒聚合窗口，默认保留 7 天。会话选择器与原始指标继续可用。
- 首次观测、有效进程启动时间变化、连续离线至少 30 秒后恢复、手动新会话时切分。短暂离线保留缺口；Counter 回退只重置速率。上游实际换模型但不修改 YAML 不会自动发现，须由使用者维护配置。
- TEI 展示 API 请求、文本/文档对、输入 Token、等待队列、请求延迟、阶段耗时和每次调度轮询平均批大小/Token 数；vLLM 非生成式页面区分 HTTP API 请求与引擎完成项。未提供指标不填零。
- SQLite 仅用于观测会话和历史，旧库的 endpoints 表不会加载、修改或删除。已有历史如需沿用，可手动在 YAML 中使用原 ID（旧表存在时可用 `SELECT id,json_extract(config,'$.url') FROM endpoints;` 查阅）。无需自动迁移配置。
- 不增加 Prometheus 服务，不集成 NVML；直接抓取后端 metrics。

TEI 指标参考 [官方 Metrics 文档](https://huggingface.co/docs/text-embeddings-inference/main/en/metrics)，vLLM 指标参考 [官方 Metrics 文档](https://docs.vllm.ai/en/latest/usage/metrics/)。

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
./vllm-monitor -config scripts/integration.yaml -db test.db
python3 scripts/integration.py
```

模拟器仅用于验证。更新后的集成测试检查 YAML-only 启动、实时速率、进程变化切分、历史保留、无元数据查询及已移除的动态配置接口；新增总览逻辑和同名模型配置测试。

## 当前实现边界

本次扩展在 v0.3 基础上增加 YAML-only、总览、TEI 和 Pooling。现有面板覆盖吞吐、Running/Waiting、完成速率、缓存、TTFT/ITL/TPOT 均值、TTFT P95 与 MTP 总体接受情况。后端也保存 Histogram bucket 增量和 Gauge 极值，支持后续扩展。

尚未完成：逐个 vLLM 版本的全部日志指标覆盖验收、MTP 各位置图、独立 Engine 明细、主题切换、可配置保留期、完整元数据清理、历史导出。扩展指标目前可在原始指标中查看；不声称全部日志已由专用图表覆盖。历史曲线当前显示窗口均值/速率，Gauge 极值仍保存在数据库。P95 为 bucket 插值估计，落入无限 bucket 时返回缺失。

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

桌面端仅监听 127.0.0.1 随机可用端口。默认在 Electron 用户数据目录使用 monitor.yaml，首次运行只创建空配置；编辑此文件并重启即可。使用 VLLM_DASHBOARD_CONFIG 指定其他 YAML 时，文件必须存在。默认数据库在 Electron 的用户数据目录（Windows 通常是 `%APPDATA%/vllm-dashboard-lite-desktop/monitor.db`）；需要和 Web 模式使用同一个数据库时，可设置绝对路径环境变量 `VLLM_DASHBOARD_DB`，并在两种模式之间先退出原进程，不要同时采集同一个数据库。关闭窗口会向后端发送退出指令，刷新聚合窗口后退出；父进程异常退出导致 stdin 关闭时，后端也会退出。

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

- 旧版按 URL/ID 固定排序，当前版本改为按 YAML 顺序。
- 增加独立桌面启动器、随机回环端口、用户数据目录、单实例窗口和后端退出管理。
- 更准确地报告启动时间缺失原因，增加无 TYPE 指标解析和相应测试。

桌面验证：`cd desktop && npm test` 检查启动协议；`npm run prepare:backend && node smoke.cjs` 检查实际 Go 子进程。当前开发环境禁止 Electron 单实例锁使用的本地 socket，原生窗口和安装器需要在目标 Windows/Linux 桌面上验证。

## v0.3 观测会话

- 会话信息分别显示“观测开始”和“进程启动”；不使用 `/v1/models.created` 推断启动时间。
- 监控程序重新启动后，首次成功采样创建新会话，旧历史保留。新会话使用独立数据库 ID，因此相同端口、相同模型、相同启动时间也能手动分开记录。
- 旧数据库自动迁移，保留原 run ID 与关联聚合数据；数据库升级后不建议用旧版程序继续写入。
- 旧版的模型/root 自动发现机制已移除。模型配置变更后重启，创建新会话。
- 上游没有启动标识时，短时间重启可能无法检测。需要严格实验边界时点击“新会话”。
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
