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
