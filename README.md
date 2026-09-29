# 软件依赖迁移工作台

第一版使用 Go 提供版本约束求解、独立核验和方案保存 API，React 提供操作页面。

## 运行

```bash
npm --prefix frontend install
npm --prefix frontend run build
go run ./cmd/server
```

打开 `http://localhost:8080`。开发模式可另开 `npm --prefix frontend run dev`。

## 能力

- 输入为 `data/catalog.json` 中的本地虚构版本资料。
- 选择一个组件和目标版本后，求解必需依赖的共同可满足版本。
- 目标之外优先保持原版本；只有约束需要时才调整其他组件。
- 可锁定当前选择后重算，冲突会返回从根到失败组件的依赖边链。
- 可选依赖未选择时不强制；一旦选择即按普通依赖核验。
- 保存方案时调用独立 verifier 重新读取选择结果，检查约束与循环依赖。

## 测试

```bash
go test ./...
npm --prefix frontend run build
```
