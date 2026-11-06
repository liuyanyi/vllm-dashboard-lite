# v0.1 验证记录

- Go 单元测试通过：身份解析、批次唯一性、Counter 重置和缺口、Histogram 解析、P95 插值。
- Go race 测试通过（单元测试范围）；go vet 通过。
- 前端 TypeScript 检查、Vite 生产构建、Vitest 2 项测试通过。
- 模拟服务集成通过：实时速率、模型信息每批次获取一次、同端口重启、模型历史隔离、窗口刷新、扫描、关闭记录。
- Playwright Chromium 1920×1080、deviceScaleFactor=1：无横向溢出、无 JS 异常、实时看板渲染和添加弹窗通过。
- Linux amd64 和 Windows amd64 交叉构建成功；Windows 二进制未在 Windows 主机实际运行。
- 尚无用户真实 vLLM 服务验证；Docker 构建未执行。

测试服务在 scripts/mock_vllm.py；它与生产服务完全独立。

# v0.2 验证记录

- 新增稳定排序测试：连续 100 次快照返回顺序一致。
- 新增启动时间状态测试：不存在、无 TYPE 有效数值、NaN、多进程冲突。
- 新增桌面后端启动握手、启动失败、超时和 stdin 优雅退出协议测试。
- 已运行真实 Go 桌面子进程 smoke 测试：随机回环端口、HTTP API 和 stdin 关闭后的零退出码通过。
- Electron 运行库已安装，但本环境禁止 Electron 单实例锁使用的本地 socket，窗口启动被系统拒绝；原生窗口和各平台安装器尚需在目标桌面系统验证。
