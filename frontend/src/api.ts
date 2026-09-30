import type { Manifest, Plan, Report, SolveRequest } from './types'

async function request<T>(url: string, options?: RequestInit): Promise<T> {
  const response = await fetch(url, { headers: { 'Content-Type': 'application/json' }, ...options })
  const data = await response.json()
  if (!response.ok) throw new Error(data.error ?? '请求失败')
  return data as T
}

export const loadSample = (scenario = '') =>
  request<SolveRequest>('/api/sample' + (scenario ? '?scenario=' + encodeURIComponent(scenario) : ''))
export const solve = (payload: SolveRequest) => request<Report>('/api/solve', { method: 'POST', body: JSON.stringify(payload) })
export const savePlan = (payload: SolveRequest) => request<Plan>('/api/plans', { method: 'POST', body: JSON.stringify(payload) })
export const listPlans = () => request<Plan[]>('/api/plans')
export const verify = (
  manifest: Manifest,
  selection: Record<string, string>,
  roots?: { target?: string; desiredVersion?: string; locks?: Record<string, string> },
) => request<Report>('/api/plans/verify', { method: 'POST', body: JSON.stringify({ manifest, selection, ...roots }) })
