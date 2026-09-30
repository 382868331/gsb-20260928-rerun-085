import { useEffect, useMemo, useState } from 'react'
import { listPlans, loadSample, savePlan, solve, verify } from './api'
import { Chain } from './Chain'
import { Compare } from './Compare'
import type { Manifest, Plan, Report, SolveRequest } from './types'

const EMPTY: SolveRequest = { manifest: { components: [] }, current: {}, target: '', desiredVersion: '', locks: {} }
const SCENARIOS: { id: string; label: string }[] = [
  { id: 'default', label: '常规升级' },
  { id: 'cycle', label: '循环依赖' },
  { id: 'lock-conflict', label: '锁定版本冲突' },
  { id: 'missing-candidate', label: '不存在候选' },
]

export default function App() {
  const [draft, setDraft] = useState<SolveRequest>(EMPTY)
  const [jsonText, setJsonText] = useState('')
  const [locksText, setLocksText] = useState('{}')
  const [report, setReport] = useState<Report | null>(null)
  const [savedReport, setSavedReport] = useState<Report | null>(null)
  const [independentReport, setIndependentReport] = useState<Report | null>(null)
  const [plans, setPlans] = useState<Plan[]>([])
  const [compareIds, setCompareIds] = useState<(string | null)[]>([null, null])
  const [currentChecks, setCurrentChecks] = useState<Record<string, Report>>({})
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    loadSample()
      .then((sample) => {
        setDraft(sample)
        setJsonText(JSON.stringify({ manifest: sample.manifest, current: sample.current }, null, 2))
      })
      .catch((err) => setError(String(err)))
    refreshPlans()
  }, [])

  async function refreshPlans() {
    try {
      setPlans(await listPlans())
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    }
  }

  async function chooseScenario(id: string) {
    setBusy(true)
    setError('')
    try {
      const sample = await loadSample(id)
      setDraft(sample)
      setJsonText(JSON.stringify({ manifest: sample.manifest, current: sample.current }, null, 2))
      setLocksText(JSON.stringify(sample.locks ?? {}))
      setReport(null)
      setSavedReport(null)
      setIndependentReport(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  const targetVersions = useMemo(() => {
    return draft.manifest.components.find((item) => item.name === draft.target)?.versions.map((item) => item.version) ?? []
  }, [draft.manifest, draft.target])

  async function run(action: 'solve' | 'save' | 'verify') {
    setBusy(true)
    setError('')
    try {
      const parsed = JSON.parse(jsonText) as Pick<SolveRequest, 'manifest' | 'current'>
      const payload: SolveRequest = {
        ...draft,
        ...parsed,
        locks: JSON.parse(locksText || '{}') as Record<string, string>,
      }
      setDraft(payload)
      if (action === 'solve') {
        const result = await solve(payload)
        setReport(result)
        setSavedReport(null)
        setIndependentReport(null)
      }
      if (action === 'save') {
        const saved = await savePlan(payload)
        setSavedReport(saved.report)
        setReport(saved.report)
        setIndependentReport(saved.verify)
        await refreshPlans()
      }
      if (action === 'verify' && report) {
        setIndependentReport(await verify(payload.manifest, report.selection, { target: payload.target, desiredVersion: payload.desiredVersion, locks: payload.locks }))
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  async function recheckWithCurrentManifest(plan: Plan) {
    setBusy(true)
    setError('')
    try {
      const parsed = JSON.parse(jsonText) as Pick<SolveRequest, 'manifest' | 'current'>
      const manifest: Manifest = parsed.manifest
      const result = await verify(manifest, plan.report.selection, { target: plan.request.target, desiredVersion: plan.request.desiredVersion, locks: plan.request.locks })
      setCurrentChecks((prev) => ({ ...prev, [plan.id]: result }))
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  const comparePlans = compareIds.map((id) => plans.find((plan) => plan.id === id) ?? null)

  return (
    <main>
      <header>
        <div>
          <p className="eyebrow">Go + React</p>
          <h1>软件依赖迁移工作台</h1>
          <p className="subtitle">先保证约束可满足，再在可行方案中减少其他组件改动；不会默认选择最新版本。</p>
        </div>
        <button onClick={() => void run('save')} disabled={busy} className="primary">保存并重新核验</button>
      </header>

      {error ? <div className="error">{error}</div> : null}

      <section className="grid">
        <div className="card">
          <h2>本地虚构版本资料</h2>
          <p className="hint">输入 manifest 和 current；后续真实包管理器格式可在此处增加适配层。</p>
          <div className="scenarios">
            {SCENARIOS.map((item) => (
              <button key={item.id} onClick={() => void chooseScenario(item.id)} disabled={busy}>{item.label}</button>
            ))}
          </div>
          <textarea value={jsonText} onChange={(event) => setJsonText(event.target.value)} spellCheck={false} />
        </div>

        <div className="card">
          <h2>升级选择与锁定</h2>
          <label>升级组件
            <input value={draft.target} onChange={(event) => setDraft({ ...draft, target: event.target.value })} />
          </label>
          <label>目标版本
            <select value={draft.desiredVersion} onChange={(event) => setDraft({ ...draft, desiredVersion: event.target.value })}>
              <option value="">请选择</option>
              {targetVersions.map((version) => <option key={version} value={version}>{version}</option>)}
            </select>
          </label>
          <label>锁定组件版本（JSON）
            <textarea className="small" value={locksText} onChange={(event) => setLocksText(event.target.value)} spellCheck={false} />
          </label>
          <div className="actions">
            <button onClick={() => void run('solve')} disabled={busy} className="primary">求解 / 重算</button>
            <button onClick={() => void run('verify')} disabled={busy || !report}>独立核验当前选择</button>
          </div>
        </div>
      </section>

      {report ? <Result report={report} saved={savedReport} independent={independentReport} /> : null}

      <PlanLibrary
        plans={plans}
        compareIds={compareIds}
        onPick={(slot, id) => setCompareIds((prev) => prev.map((value, index) => (index === slot ? id : value)))}
        comparePlans={comparePlans}
        currentChecks={currentChecks}
        onRecheck={(plan) => void recheckWithCurrentManifest(plan)}
        busy={busy}
      />
    </main>
  )
}

function Result({ report, saved, independent }: { report: Report; saved: Report | null; independent: Report | null }) {
  const consistency = saved && independent
    ? saved.status === independent.status && JSON.stringify(Object.entries(saved.selection).sort()) === JSON.stringify(Object.entries(independent.selection).sort())
    : null

  return (
    <section className="result-grid">
      <div className={'card verdict ' + report.status}>
        <h2>{report.status === 'feasible' ? '方案可满足' : '存在冲突'}</h2>
        {report.conflictMessage ? <p>{report.conflictMessage}</p> : <p>共需调整 {report.changedCount} 个组件。</p>}
        <Chain links={report.conflictChain} />
      </div>

      <div className="card">
        <h2>必须一起调整</h2>
        <table>
          <thead><tr><th>组件</th><th>当前</th><th>方案</th><th>原因</th></tr></thead>
          <tbody>
            {report.changes.map((change) => (
              <tr key={change.component}><td>{change.component}</td><td>{change.from}</td><td>{change.to}</td><td>{change.reason}</td></tr>
            ))}
          </tbody>
        </table>
        {report.optionalOmitted?.length ? <p className="warning-text">已跳过不可满足的可选依赖：{report.optionalOmitted.join('、')}</p> : null}
      </div>

      <div className="card">
        <h2>最终选择</h2>
        <div className="selection">
          {Object.entries(report.selection).sort().map(([name, version]) => <span key={name}>{name + '@' + version}</span>)}
        </div>
      </div>

      <div className="card">
        <h2>循环依赖与可选依赖检查</h2>
        {report.checks.length === 0 ? <p className="hint">暂无检查项。</p> : report.checks.map((check, index) => (
          <div key={index} className={'check ' + check.status}>
            <strong>{check.kind} / {check.status}</strong>
            <p>{check.message}</p>
            <Chain links={check.chain} />
          </div>
        ))}
        {consistency === true ? <p className="success-text">页面结论与独立核验一致。</p> : null}
        {consistency === false ? <p className="error-text">页面结论与独立核验不一致。</p> : null}
      </div>
    </section>
  )
}

function PlanLibrary({
  plans,
  compareIds,
  onPick,
  comparePlans,
  currentChecks,
  onRecheck,
  busy,
}: {
  plans: Plan[]
  compareIds: (string | null)[]
  onPick: (slot: number, id: string) => void
  comparePlans: (Plan | null)[]
  currentChecks: Record<string, Report>
  onRecheck: (plan: Plan) => void
  busy: boolean
}) {
  return (
    <section className="plan-library">
      <div className="card">
        <h2>已保存方案（保存时快照，不随后续改约束而改变）</h2>
        {plans.length === 0 ? <p className="hint">还没有保存方案。</p> : (
          <table>
            <thead><tr><th>比较</th><th>ID</th><th>目标</th><th>求解</th><th>独立核验</th><th>选择快照</th><th>按当前资料复核</th><th>保存时间</th></tr></thead>
            <tbody>
              {[...plans].reverse().map((plan) => {
                const current = currentChecks[plan.id]
                return (
                  <tr key={plan.id}>
                    <td>
                      <label className="pick"><input type="radio" name="compare-a" checked={compareIds[0] === plan.id} onChange={() => onPick(0, plan.id)} />A</label>
                      <label className="pick"><input type="radio" name="compare-b" checked={compareIds[1] === plan.id} onChange={() => onPick(1, plan.id)} />B</label>
                    </td>
                    <td>{plan.id.slice(0, 8)}</td>
                    <td>{plan.request.target} @ {plan.request.desiredVersion}</td>
                    <td className={plan.report.status}>{plan.report.status}</td>
                    <td className={plan.verify.status}>{plan.verify.status}{plan.consistent ? '' : '（与求解不一致）'}</td>
                    <td className="snapshot">{Object.entries(plan.report.selection).sort().map(([name, version]) => name + '@' + version).join('，') || '—'}</td>
                    <td>
                      <button onClick={() => onRecheck(plan)} disabled={busy}>用当前资料复核</button>
                      {current ? <span className={'badge ' + current.status}>{current.status}</span> : null}
                    </td>
                    <td>{plan.createdAt}</td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        )}
        <p className="hint">复核只使用页面上当前编辑的 manifest 重新检查保存时的选择快照，旧方案本身的版本不会被改写。</p>
      </div>

      <div className="card">
        <h2>方案比较：实际改变与冲突依赖链</h2>
        <Compare left={comparePlans[0]} right={comparePlans[1]} />
      </div>
    </section>
  )
}
