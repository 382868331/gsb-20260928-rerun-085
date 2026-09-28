# 软件依赖迁移工作台

第一版使用 Go 实现依赖求解与独立核验，React 提供操作页面。输入为本地虚构版本资料，后续真实包管理器格式可在 HTTP 层之外新增适配器，统一转换成内部 Manifest。

## 能力

- 选择一个组件和目标版本后，求解必须一起调整的组件。
- 先搜索满足所有非可选依赖约束的方案，再比较组件改动数量。
- 改动数量相同时选择最低可行版本，不默认追最新版本。
- 支持锁定部分组件版本并重新求解。
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

- GET /api/sample：获取虚构样例。
- POST /api/solve：求解迁移方案。
- POST /api/plans：保存方案，并执行独立核验。
- POST /api/plans/verify：只对给定选择执行独立核验。
- GET /api/plans：查看已保存方案。

## 测试

- go test ./...
- npm run build --prefix frontend
