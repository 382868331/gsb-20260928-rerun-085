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
export type RollbackInfo = { state: Record<string, string>; irreversible?: string[] }
export type StepCheck = { name: string; expect: string }
export type CheckRecord = { name: string; result: 'success' | 'failure' | 'insufficient-data' | string; note?: string }
export type RouteOption = { kind: string; title: string; state?: Record<string, string>; unresolved?: string[] }
export type RouteStep = {
  index: number
  changes: Change[]
  state: Record<string, string>
  together?: string[]
  transition?: string[]
  note?: string
  verification: Report
  rollback: RollbackInfo
  confirmed?: boolean
  checks?: StepCheck[]
  records?: CheckRecord[]
  executed?: boolean
  acceptedFailure?: boolean
}
export type RouteRequest = {
  manifest: Manifest
  current: Record<string, string>
  target?: string
  desiredVersion?: string
  goals?: Array<{ component: string; version: string }>
  locks?: Record<string, string>
  confirmed?: RouteStep[]
}
export type RouteReport = {
  status: 'feasible' | 'conflict' | string
  resumedFrom: Record<string, string>
  confirmedSteps?: RouteStep[]
  steps: RouteStep[]
  finalSelection?: Record<string, string>
  conflictMessage?: string
  remainingConflicts?: Check[]
  pendingStep?: RouteStep
  options?: RouteOption[]
}
export type SavedRoute = {
  id: string
  request: RouteRequest
  report: RouteReport
  createdAt: string
}
