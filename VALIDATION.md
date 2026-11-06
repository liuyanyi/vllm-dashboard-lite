# v0.1 验证记录

- Go 单元测试通过：身份解析、批次唯一性、Counter 重置和缺口、Histogram 解析、P95 插值。
- Go race 测试通过（单元测试范围）；go vet 通过。
- 前端 TypeScript 检查、Vite 生产构建、Vitest 2 项测试通过。
- 模拟服务集成通过：实时速率、模型信息每批次获取一次、同端口重启、模型历史隔离、窗口刷新、扫描、关闭记录。
- Playwright Chromium 1920×1080、deviceScaleFactor=1：无横向溢出、无 JS 异常、实时看板渲染和添加弹窗通过。
- Linux amd64 和 Windows amd64 交叉构建成功；Windows 二进制未在 Windows 主机实际运行。
- 尚无用户真实 vLLM 服务验证；Docker 构建未执行。

测试服务在 scripts/mock_vllm.py；它与生产服务完全独立。
