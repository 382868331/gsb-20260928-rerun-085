import type { Catalog, Plan, Report } from './types';

async function request<T>(url: string, options?: RequestInit): Promise<T> {
  const response = await fetch(url, {
    headers: { 'Content-Type': 'application/json' },
    ...options
  });
  if (!response.ok) {
    const body = await response.json().catch(() => ({}));
    throw new Error(body.error ?? `请求失败：${response.status}`);
  }
  return response.json();
}

export const getCatalog = () => request<Catalog>('/api/catalog');

export function solve(payload: {
  targetComponent: string;
  targetVersion: string;
  current: Record<string, string>;
  locks: Record<string, string>;
}) {
  return request<Report>('/api/solve', {
    method: 'POST',
    body: JSON.stringify(payload)
  });
}

export function verify(payload: {
  choices: Record<string, string>;
  current: Record<string, string>;
  target: string;
}) {
  return request<Report>('/api/verify', {
    method: 'POST',
    body: JSON.stringify(payload)
  });
}

export const listPlans = () => request<Plan[]>('/api/plans');

export function savePlan(plan: Plan) {
  return request<{ plan: Plan; report: Report }>('/api/plans', {
    method: 'POST',
    body: JSON.stringify(plan)
  });
}
