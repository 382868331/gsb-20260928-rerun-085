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
