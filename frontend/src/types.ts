export type Requirement = { component: string; constraint: string; optional?: boolean }
export type Version = { version: string; requirements?: Requirement[] }
export type Component = { name: string; versions: Version[] }
export type Manifest = { components: Component[] }
export type SolveRequest = {
  manifest: Manifest
  current: Record<string, string>
  target: string
  desiredVersion: string
  locks?: Record<string, string>
  goals?: Array<{ component: string; version: string }>
  confirmed?: Record<string, string>
}
export type GoalStatus = { component: string; required: string; selected?: string; status: 'satisfied' | 'pending' | 'unmet' | string; reason?: string }
export type ChainLink = { from: string; fromVersion?: string; to: string; constraint: string; optional?: boolean }
export type Change = { component: string; from: string; to: string; reason: string }
export type Check = { kind: string; status: string; message: string; chain?: ChainLink[] }
export type Report = {
  status: 'feasible' | 'conflict' | string
  selection: Record<string, string>
  goalStatuses?: GoalStatus[]
  changes: Change[]
  changedCount: number
  conflictChain?: ChainLink[]
  conflictMessage?: string
  checks: Check[]
  optionalOmitted?: string[]
}
export type Scenario = {
  id: string
  name: string
  description: string
  request: SolveRequest
}
export type Plan = {
  id: string
  request: SolveRequest
  report: Report
  verification: Report
  consistent: boolean
  createdAt: string
}
export type RouteStep = {
  index: number
  changes: Change[]
  state: Record<string, string>
  verification: Report
  transitional?: boolean
  together?: boolean
  reason?: string
}
export type RollbackPoint = {
  step: number
  state: Record<string, string>
  revertible: boolean
  reason?: string
}
export type RouteRevision = {
  confirmedSteps: number
  goals: Array<{ component: string; version: string }>
  locks?: Record<string, string>
  note: string
  at: string
}
export type Route = {
  id: string
  request: SolveRequest
  steps: RouteStep[]
  confirmed: number
  status: string
  finalSelection?: Record<string, string>
  conflictMessage?: string
  conflictChain?: ChainLink[]
  rollback?: RollbackPoint[]
  revisions?: RouteRevision[]
  createdAt: string
  updatedAt: string
}
export type RoutePlan = {
  steps: RouteStep[]
  status: string
  finalSelection?: Record<string, string>
  conflictMessage?: string
  conflictChain?: ChainLink[]
}
