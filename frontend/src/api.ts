import type { Manifest, Report, SolveRequest } from './types'

async function request<T>(url: string, options?: RequestInit): Promise<T> {
  const response = await fetch(url, { headers: { 'Content-Type': 'application/json' }, ...options })
  const data = await response.json()
  if (!response.ok) throw new Error(data.error ?? '请求失败')
  return data as T
}

export const loadSample = () => request<SolveRequest>('/api/sample')
export const solve = (payload: SolveRequest) => request<Report>('/api/solve', { method: 'POST', body: JSON.stringify(payload) })
export const savePlan = (payload: SolveRequest) => request<{ id: string; report: Report }>('/api/plans', { method: 'POST', body: JSON.stringify(payload) })
export const verify = (manifest: Manifest, selection: Record<string, string>) =>
  request<Report>('/api/plans/verify', { method: 'POST', body: JSON.stringify({ manifest, selection }) })
