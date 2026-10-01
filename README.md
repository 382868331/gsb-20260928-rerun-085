# 软件依赖迁移工作台

第一版使用 Go 实现依赖求解与独立核验，React 提供操作页面。输入为本地虚构版本资料，后续真实包管理器格式可在 HTTP 层之外新增适配器，统一转换成内部 Manifest。

## 能力

- 选择一个组件和目标版本后，求解必须一起调整的组件。
- 先搜索满足所有非可选依赖约束的方案，再比较组件改动数量。
- 改动数量相同时选择最低可行版本，不默认追最新版本。
- 支持锁定部分组件版本并重新求解。
- 支持部分采纳方案：已确认组件可通过 `confirmed`（或现有 `locks`）固定，求解器继续为剩余迁移目标寻找兼容组合。
- 独立核验逐项输出迁移目标状态：`satisfied`、`pending`、`unmet`，空选择或未提供目标都不能证明覆盖成功。
- 冲突报告包含依赖链，定位哪个组件版本要求了不兼容版本。
- 可选依赖无法满足时允许省略并给出警告；循环依赖作为独立检查警告。
- 保存方案后调用独立 verifier 重新核验，页面可再次调用核验接口比较结论。

## 运行

在仓库根目录执行：

1. npm install
2. npm run build --prefix frontend
3. go run ./cmd/server

然后打开 http://localhost:8080。

开发模式可分别运行 go run ./cmd/server 和 npm run dev --prefix frontend。

## API

- GET /api/scenarios：列出内置资料（常规升级、循环依赖、锁定版本冲突、不存在候选）。
- GET /api/sample?id=base：获取指定资料的完整求解请求。
- POST /api/solve：求解迁移方案，冲突时返回从升级请求/锁定出发的完整依赖链。
- POST /api/plans：保存方案；服务端冻结请求中的 manifest 快照，并调用独立 verifier 重新核验，返回 consistent 结论，不采信求解阶段标记。
- POST /api/plans/verify：对完整迁移请求（manifest、目标、目标版本/目标列表、确认组件）与选择执行独立核验。
- GET /api/plans：查看已保存方案（含快照与独立核验报告）。

保存后即使修改页面上的约束，旧方案仍保留保存时的 manifest 副本、版本选择和核验结论；可在页面上对同一批方案使用不同锁定进行并排对比。

计划文件默认写入工作目录的 plans.json，可用环境变量 PLANS_PATH 覆盖；监听地址用 ADDR（默认 :8080）。

## 测试

- go test ./...
- npm run build --prefix frontend
