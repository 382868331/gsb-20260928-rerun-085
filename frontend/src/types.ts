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
export type StepCheck = { id: string; label: string; status: 'pending' | 'success' | 'failed' | 'insufficient' | string; note?: string }
export type RouteStep = {
  index: number
  changes: Change[]
  state: Record<string, string>
  together?: string[]
  transition?: string[]
  note?: string
  checks?: StepCheck[]
  verification: Report
  rollback: RollbackInfo
  confirmed?: boolean
  // executed: the conversion physically ran (recorded without a failed
  // outcome); independent of whether every check passed. 资料不足 keeps
  // the step executed but not effective.
  executed?: boolean
  effective?: boolean
}
export type RouteOption = { kind: 'continue' | 'rollback' | 'stop' | string; summary: string; state: Record<string, string>; impacts: string[] }
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
  heldSteps?: RouteStep[]
  steps: RouteStep[]
  finalSelection?: Record<string, string>
  conflictMessage?: string
  remainingConflicts?: Check[]
  options?: RouteOption[]
}
export type SavedRoute = {
  id: string
  fingerprint?: string
  request: RouteRequest
  report: RouteReport
  // executedState: the running set actually reached (every executed step,
  // held ones included), stored separately from the plan; irreversible:
  // data impacts already incurred, including steps whose checks failed.
  executedState?: Record<string, string>
  irreversible?: string[]
  createdAt: string
}
export type ConfirmRequest = { routeId: string; stepIndex: number; checks: StepCheck[] }
