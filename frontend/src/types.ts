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
}
export type ChainLink = { from: string; fromVersion?: string; to: string; constraint: string; optional?: boolean }
export type Change = { component: string; from: string; to: string; reason: string }
export type Check = { kind: string; status: string; message: string; chain?: ChainLink[] }
export type Report = {
  status: 'feasible' | 'conflict' | string
  selection: Record<string, string>
  changes: Change[]
  changedCount: number
  conflictChain?: ChainLink[]
  conflictMessage?: string
  checks: Check[]
  optionalOmitted?: string[]
}
