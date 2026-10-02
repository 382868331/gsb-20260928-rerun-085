import type { Manifest, Plan, Report, Route, RoutePlan, Scenario, SolveRequest } from './types'

async function request<T>(url: string, options?: RequestInit): Promise<T> {
  const response = await fetch(url, { headers: { 'Content-Type': 'application/json' }, ...options })
  const data = await response.json()
  if (!response.ok) throw new Error(data.error ?? '请求失败')
  return data as T
}

export const loadScenarios = () => request<Scenario[]>('/api/scenarios')
export const loadSample = (id: string) => request<Scenario>(`/api/sample?id=${encodeURIComponent(id)}`)
export const solve = (payload: SolveRequest) => request<Report>('/api/solve', { method: 'POST', body: JSON.stringify(payload) })
export const savePlan = (payload: SolveRequest) => request<Plan>('/api/plans', { method: 'POST', body: JSON.stringify(payload) })
export const listPlans = () => request<Plan[]>('/api/plans')
export const verify = (payload: SolveRequest, selection: Record<string, string>) =>
  request<Report>('/api/plans/verify', { method: 'POST', body: JSON.stringify({ ...payload, selection }) })
export const planRoute = (payload: SolveRequest) => request<RoutePlan>('/api/routes/plan', { method: 'POST', body: JSON.stringify(payload) })
export const saveRoute = (payload: SolveRequest) => request<Route>('/api/routes', { method: 'POST', body: JSON.stringify(payload) })
export const listRoutes = () => request<Route[]>('/api/routes')
export const advanceRoute = (payload: { id: string; confirm: number; target?: string; desiredVersion?: string; locks?: Record<string, string> }) =>
  request<Route>('/api/routes/advance', { method: 'POST', body: JSON.stringify(payload) })
