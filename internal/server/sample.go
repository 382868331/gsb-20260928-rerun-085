package server

import "dependency-workbench/internal/domain"

// Scenario bundles a ready-to-solve request with a short description.
type Scenario struct {
	ID          string              `json:"id"`
	Name        string              `json:"name"`
	Description string              `json:"description"`
	Request     domain.SolveRequest `json:"request"`
}

func scenarios() []Scenario {
	return []Scenario{
		{
			ID:          "base",
			Name:        "常规升级：最少改动",
			Description: "app 升级到 2.0.0，lib 只需提升到最低可行版本；legacy 是不可满足的可选依赖，允许跳过并警告。",
			Request: domain.SolveRequest{
				Manifest: domain.Manifest{Components: []domain.Component{
					{Name: "app", Versions: []domain.Version{
						{Version: "1.0.0", Requirements: []domain.Requirement{{Component: "lib", Constraint: "^1.0.0"}, {Component: "legacy", Constraint: "^1.0.0", Optional: true}}},
						{Version: "2.0.0", Requirements: []domain.Requirement{{Component: "lib", Constraint: "^2.0.0"}, {Component: "legacy", Constraint: "^9.0.0", Optional: true}}},
					}},
					{Name: "lib", Versions: []domain.Version{
						{Version: "1.0.0"}, {Version: "1.5.0"}, {Version: "2.0.0"}, {Version: "2.1.0"}, {Version: "3.0.0"},
					}},
					{Name: "legacy", Versions: []domain.Version{{Version: "1.0.0"}}},
					{Name: "broken", Versions: []domain.Version{
						{Version: "1.0.0", Requirements: []domain.Requirement{{Component: "lib", Constraint: "^3.0.0"}}},
						{Version: "2.0.0", Requirements: []domain.Requirement{{Component: "lib", Constraint: "^1.0.0"}}},
					}},
				}},
				Current:        map[string]string{"app": "1.0.0", "lib": "1.5.0"},
				Target:         "app",
				DesiredVersion: "2.0.0",
				Locks:          map[string]string{},
			},
		},
		{
			ID:          "cycle",
			Name:        "循环依赖：独立检查警告",
			Description: "a 与 b 互相要求对方 ^1.0.0，方案可满足，但 verifier 必须把循环依赖作为警告并给出依赖链。",
			Request: domain.SolveRequest{
				Manifest: domain.Manifest{Components: []domain.Component{
					{Name: "a", Versions: []domain.Version{
						{Version: "1.0.0", Requirements: []domain.Requirement{{Component: "b", Constraint: "^1.0.0"}}},
					}},
					{Name: "b", Versions: []domain.Version{
						{Version: "1.0.0", Requirements: []domain.Requirement{{Component: "a", Constraint: "^1.0.0"}}},
					}},
				}},
				Current:        map[string]string{"a": "1.0.0", "b": "1.0.0"},
				Target:         "a",
				DesiredVersion: "1.0.0",
				Locks:          map[string]string{},
			},
		},
		{
			ID:          "lock-conflict",
			Name:        "锁定版本冲突：依赖链定位",
			Description: "app 2.0.0 要求 lib ^2.0.0，但手动锁定 lib 3.0.0，求解必须失败并展示 upgrade request -> app -> lib 的冲突链。",
			Request: domain.SolveRequest{
				Manifest: domain.Manifest{Components: []domain.Component{
					{Name: "app", Versions: []domain.Version{
						{Version: "1.0.0", Requirements: []domain.Requirement{{Component: "lib", Constraint: "^1.0.0"}}},
						{Version: "2.0.0", Requirements: []domain.Requirement{{Component: "lib", Constraint: "^2.0.0"}}},
					}},
					{Name: "lib", Versions: []domain.Version{
						{Version: "1.0.0"}, {Version: "2.0.0"}, {Version: "2.1.0"}, {Version: "3.0.0"},
					}},
				}},
				Current:        map[string]string{"app": "1.0.0", "lib": "1.0.0"},
				Target:         "app",
				DesiredVersion: "2.0.0",
				Locks:          map[string]string{"lib": "3.0.0"},
			},
		},
		{
			ID:          "missing-candidate",
			Name:        "不存在候选：缺失组件",
			Description: "ghost 1.0.0 依赖 phantom ^1.0.0，但资料里根本没有 phantom，求解必须报告无候选而非编造版本。",
			Request: domain.SolveRequest{
				Manifest: domain.Manifest{Components: []domain.Component{
					{Name: "ghost", Versions: []domain.Version{
						{Version: "1.0.0", Requirements: []domain.Requirement{{Component: "phantom", Constraint: "^1.0.0"}}},
					}},
				}},
				Current:        map[string]string{},
				Target:         "ghost",
				DesiredVersion: "1.0.0",
				Locks:          map[string]string{},
			},
		},
		{
			ID:          "mutual",
			Name:        "相互依赖升级：必须同步切换",
			Description: "app 2.0.0 要求 lib ^2.0.0，lib 2.0.0 也要求 app ^2.0.0；分阶段路线必须把两者放进同一步，单独升级任一方都没有可行中间状态。",
			Request: domain.SolveRequest{
				Manifest: domain.Manifest{Components: []domain.Component{
					{Name: "app", Versions: []domain.Version{
						{Version: "1.0.0", Requirements: []domain.Requirement{{Component: "lib", Constraint: "^1.0.0"}}},
						{Version: "2.0.0", Requirements: []domain.Requirement{{Component: "lib", Constraint: "^2.0.0"}}},
					}},
					{Name: "lib", Versions: []domain.Version{
						{Version: "1.0.0"},
						{Version: "2.0.0", Requirements: []domain.Requirement{{Component: "app", Constraint: "^2.0.0"}}},
					}},
				}},
				Current:        map[string]string{"app": "1.0.0", "lib": "1.0.0"},
				Target:         "app",
				DesiredVersion: "2.0.0",
				Locks:          map[string]string{},
			},
		},
		{
			ID:          "transition",
			Name:        "过渡版本：分步到位",
			Description: "单步不允许跨多个大版本，且 lib 3.0.0 要求 app >= 2.0.0、app 2.0.0 要求 lib ^2.0.0。路线需先同步切到过渡版本 app 2.0.0 + lib 2.0.0，再升 lib 3.0.0，最后升 app 3.0.0。",
			Request: domain.SolveRequest{
				Manifest: domain.Manifest{Components: []domain.Component{
					{Name: "app", Versions: []domain.Version{
						{Version: "1.0.0", Requirements: []domain.Requirement{{Component: "lib", Constraint: "^1.0.0"}}},
						{Version: "2.0.0", Requirements: []domain.Requirement{{Component: "lib", Constraint: "^2.0.0"}}},
						{Version: "3.0.0", Requirements: []domain.Requirement{{Component: "lib", Constraint: "^3.0.0"}}},
					}},
					{Name: "lib", Versions: []domain.Version{
						{Version: "1.0.0"},
						{Version: "2.0.0", Requirements: []domain.Requirement{{Component: "app", Constraint: "^2.0.0"}}},
						{Version: "3.0.0", Requirements: []domain.Requirement{{Component: "app", Constraint: ">=2.0.0"}}},
					}},
				}},
				Current:        map[string]string{"app": "1.0.0", "lib": "1.0.0"},
				Target:         "app",
				DesiredVersion: "3.0.0",
				Locks:          map[string]string{},
			},
		},
		{
			ID:          "optional-exit",
			Name:        "可选组件退出：不可直接撤回",
			Description: "cache 是 app 1.0.0 的可选依赖，app 2.0.0 不再需要它。路线会安排 cache 退出运行状态，并标注该影响不可直接撤回（数据与配置需手动处理）。",
			Request: domain.SolveRequest{
				Manifest: domain.Manifest{Components: []domain.Component{
					{Name: "app", Versions: []domain.Version{
						{Version: "1.0.0", Requirements: []domain.Requirement{{Component: "lib", Constraint: "^1.0.0"}, {Component: "cache", Constraint: "^1.0.0", Optional: true}}},
						{Version: "2.0.0", Requirements: []domain.Requirement{{Component: "lib", Constraint: "^2.0.0"}}},
					}},
					{Name: "lib", Versions: []domain.Version{
						{Version: "1.0.0"},
						{Version: "2.0.0"},
					}},
					{Name: "cache", Versions: []domain.Version{{Version: "1.0.0"}}},
				}},
				Current:        map[string]string{"app": "1.0.0", "lib": "1.0.0", "cache": "1.0.0"},
				Target:         "app",
				DesiredVersion: "2.0.0",
				Locks:          map[string]string{},
			},
		},
		{
			ID:          "mid-lock",
			Name:        "中途锁定组件：剩余路线随之改变",
			Description: "以 app 3.0.0 为目标规划并确认第一步（app 2.0.0 + lib 2.0.0）后，把 lib 锁定在 2.0.0；app 3.0.0 要求 lib ^3.0.0，路线必须报告剩余冲突而不是推翻已执行步骤。",
			Request: domain.SolveRequest{
				Manifest: domain.Manifest{Components: []domain.Component{
					{Name: "app", Versions: []domain.Version{
						{Version: "1.0.0", Requirements: []domain.Requirement{{Component: "lib", Constraint: "^1.0.0"}}},
						{Version: "2.0.0", Requirements: []domain.Requirement{{Component: "lib", Constraint: "^2.0.0"}}},
						{Version: "3.0.0", Requirements: []domain.Requirement{{Component: "lib", Constraint: "^3.0.0"}}},
					}},
					{Name: "lib", Versions: []domain.Version{
						{Version: "1.0.0"},
						{Version: "2.0.0", Requirements: []domain.Requirement{{Component: "app", Constraint: "^2.0.0"}}},
						{Version: "3.0.0", Requirements: []domain.Requirement{{Component: "app", Constraint: "^2.0.0"}}},
					}},
				}},
				Current:        map[string]string{"app": "1.0.0", "lib": "1.0.0"},
				Target:         "app",
				DesiredVersion: "3.0.0",
				Locks:          map[string]string{},
			},
		},
		{
			ID:          "goal-change",
			Name:        "中途改变目标：从已完成状态继续",
			Description: "先以 app 3.0.0 为目标规划并确认第一步（app 2.0.0 + lib 2.0.0），再把目标改成 app 2.0.0 或锁定 lib；后续计划只从已完成状态继续，历史步骤保持原样。",
			Request: domain.SolveRequest{
				Manifest: domain.Manifest{Components: []domain.Component{
					{Name: "app", Versions: []domain.Version{
						{Version: "1.0.0", Requirements: []domain.Requirement{{Component: "lib", Constraint: "^1.0.0"}}},
						{Version: "2.0.0", Requirements: []domain.Requirement{{Component: "lib", Constraint: "^2.0.0"}}},
						{Version: "3.0.0", Requirements: []domain.Requirement{{Component: "lib", Constraint: "^3.0.0"}}},
					}},
					{Name: "lib", Versions: []domain.Version{
						{Version: "1.0.0"},
						{Version: "2.0.0", Requirements: []domain.Requirement{{Component: "app", Constraint: "^2.0.0"}}},
						{Version: "3.0.0", Requirements: []domain.Requirement{{Component: "app", Constraint: "^2.0.0"}}},
					}},
				}},
				Current:        map[string]string{"app": "1.0.0", "lib": "1.0.0"},
				Target:         "app",
				DesiredVersion: "3.0.0",
				Locks:          map[string]string{},
			},
		},
	}
}

func scenarioByID(id string) (Scenario, bool) {
	if id == "" {
		id = "base"
	}
	for _, item := range scenarios() {
		if item.ID == id {
			return item, true
		}
	}
	return Scenario{}, false
}
