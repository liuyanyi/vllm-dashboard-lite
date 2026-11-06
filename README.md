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
- 单层服务列表，模型自动识别；端点 + `process_start_time_seconds` 唯一确定批次。
- 模型信息每批次成功获取一次；失败后每 15 秒重试。没有启动时间时只实时显示，不写入历史。
- 1 秒默认采样，10 秒吞吐窗口，60 秒延迟窗口，内存保留 30 分钟。首次需要两个有效样本才能显示速率。
- 打开“记录”保存 30 秒聚合窗口，默认保留 7 天；关闭记录会刷新当前有效窗口。不保存开启记录之前的内存数据。
- 批次选择器查看历史，原始指标查看全部已解析 Gauge/Counter/Histogram 序列。
- 离线期间保留缺口；Counter 回退不新建批次；模型名称变化本身不新建批次。

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

Go 测试覆盖启动时间解析、Counter 回退/采集缺口、批次唯一性和 Histogram 解析；Vitest 覆盖前端缓冲区去重、过期和缺失值。

集成测试需要干净数据库，按以下顺序启动：

```bash
python3 scripts/mock_vllm.py
./vllm-monitor -db test.db
python3 scripts/integration.py
```

模拟器只用于验证，真实服务不注入任何模拟数据。集成测试验证模型信息只获取一次、同端口重启切分、旧批次模型不变、历史刷新、扫描和记录开关。

## 当前实现边界

这是可运行的 v0.1 首版。现有面板覆盖吞吐、Running/Waiting、完成速率、缓存、TTFT/ITL/TPOT 均值、TTFT P95 与 MTP 总体接受情况。后端也保存 Histogram bucket 增量和 Gauge 极值，支持后续扩展。

尚未完成：逐个 vLLM 版本的全部日志指标覆盖验收、MTP 各位置图、独立 Engine 明细、自动周期扫描、主题切换、可配置保留期、完整元数据清理、历史导出。扩展指标目前可在原始指标中查看；不声称全部日志已由专用图表覆盖。历史曲线当前显示窗口均值/速率，Gauge 极值仍保存在数据库。P95 为 bucket 插值估计，落入无限 bucket 时返回缺失。

只支持直接连接单个实例；多 API Server 不提供 process_start_time_seconds 时降级实时模式，不猜测身份。窗口聚合以有效采样间隔归入窗口，约 30 秒，不保证整点边界。实际断连与进程重启只能在下一次采样时观察到。

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

Dashboard 支持 Gauge 及省略 TYPE 的数值字段，并区分未暴露、无效值、多个不同启动时间。字段缺失时继续实时观察；按既定规则，历史记录保持不可用，不引入推断批次。

参考：[vLLM registry 源码](https://docs.vllm.ai/en/latest/api/vllm/v1/metrics/prometheus/)、[进程指标说明](https://docs.vllm.ai/en/stable/design/metrics/)。

## v0.2 修改

- 服务列表按端点 URL、ID 固定排序，不受 map 遍历、状态变化或 SSE 刷新影响。
- 增加独立桌面启动器、随机回环端口、用户数据目录、单实例窗口和后端退出管理。
- 更准确地报告启动时间缺失原因，增加无 TYPE 指标解析和相应测试。

桌面验证：`cd desktop && npm test` 检查启动协议；`npm run prepare:backend && node smoke.cjs` 检查实际 Go 子进程。当前开发环境禁止 Electron 单实例锁使用的本地 socket，原生窗口和安装器需要在目标 Windows/Linux 桌面上验证。
