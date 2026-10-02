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
			ID:          "staged-transition",
			Name:        "分阶段路线：过渡版本",
			Description: "top 升到 2.0.0 时 mid 必须先到过渡版 1.5.0 才能让 core 升级，最后 mid 与 top 必须一起切换；每个中间状态都可独立核验。",
			Request: domain.SolveRequest{
				Manifest: domain.Manifest{Components: []domain.Component{
					{Name: "top", Versions: []domain.Version{
						{Version: "1.0.0", Requirements: []domain.Requirement{{Component: "mid", Constraint: "^1.0.0"}}},
						{Version: "2.0.0", Requirements: []domain.Requirement{{Component: "mid", Constraint: "^2.0.0"}}},
					}},
					{Name: "mid", Versions: []domain.Version{
						{Version: "1.0.0", Requirements: []domain.Requirement{{Component: "core", Constraint: "^1.0.0"}}},
						{Version: "1.5.0", Requirements: []domain.Requirement{{Component: "core", Constraint: ">=1.0.0 <3.0.0"}}},
						{Version: "2.0.0", Requirements: []domain.Requirement{{Component: "core", Constraint: "^2.0.0"}}},
					}},
					{Name: "core", Versions: []domain.Version{{Version: "1.0.0"}, {Version: "2.0.0"}}},
				}},
				Current:        map[string]string{"top": "1.0.0", "mid": "1.0.0", "core": "1.0.0"},
				Target:         "top",
				DesiredVersion: "2.0.0",
				Locks:          map[string]string{},
			},
		},
		{
			ID:          "interdependent",
			Name:        "相互依赖升级：一起切换",
			Description: "app 与 lib 互相约束对方主版本，单独升级任一方都会破坏运行状态，路线必须把它们放进同一步一起切换。",
			Request: domain.SolveRequest{
				Manifest: domain.Manifest{Components: []domain.Component{
					{Name: "app", Versions: []domain.Version{
						{Version: "1.0.0", Requirements: []domain.Requirement{{Component: "lib", Constraint: "^1.0.0"}}},
						{Version: "2.0.0", Requirements: []domain.Requirement{{Component: "lib", Constraint: "^2.0.0"}}},
					}},
					{Name: "lib", Versions: []domain.Version{
						{Version: "1.0.0", Requirements: []domain.Requirement{{Component: "app", Constraint: "^1.0.0"}}},
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
			ID:          "optional-exit",
			Name:        "可选组件退出",
			Description: "portal 2.0.0 的可选依赖 plugin ^2.0.0 在资料中不存在，路线允许其退出并在该步的独立核验中给出警告而非失败。",
			Request: domain.SolveRequest{
				Manifest: domain.Manifest{Components: []domain.Component{
					{Name: "portal", Versions: []domain.Version{
						{Version: "1.0.0", Requirements: []domain.Requirement{{Component: "plugin", Constraint: "^1.0.0", Optional: true}}},
						{Version: "2.0.0", Requirements: []domain.Requirement{{Component: "plugin", Constraint: "^2.0.0", Optional: true}}},
					}},
					{Name: "plugin", Versions: []domain.Version{{Version: "1.0.0"}}},
				}},
				Current:        map[string]string{"portal": "1.0.0", "plugin": "1.0.0"},
				Target:         "portal",
				DesiredVersion: "2.0.0",
				Locks:          map[string]string{},
			},
		},
		{
			ID:          "goal-change",
			Name:        "中途改变目标",
			Description: "svc 经 1.5.0 过渡到 2.0.0；确认第一步后可将目标改为 svc 2.1.0 或锁定 db，剩余步骤只从已确认状态继续规划。",
			Request: domain.SolveRequest{
				Manifest: domain.Manifest{Components: []domain.Component{
					{Name: "svc", Versions: []domain.Version{
						{Version: "1.0.0", Requirements: []domain.Requirement{{Component: "db", Constraint: "^1.0.0"}}},
						{Version: "1.5.0", Requirements: []domain.Requirement{{Component: "db", Constraint: ">=1.0.0 <3.0.0"}}},
						{Version: "2.0.0", Requirements: []domain.Requirement{{Component: "db", Constraint: "^2.0.0"}}},
						{Version: "2.1.0", Requirements: []domain.Requirement{{Component: "db", Constraint: "^2.0.0"}}},
					}},
					{Name: "db", Versions: []domain.Version{{Version: "1.0.0"}, {Version: "2.0.0"}}},
				}},
				Current:        map[string]string{"svc": "1.0.0", "db": "1.0.0"},
				Target:         "svc",
				DesiredVersion: "2.0.0",
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
