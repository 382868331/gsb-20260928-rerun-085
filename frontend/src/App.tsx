import { useEffect, useMemo, useState } from 'react'
import { loadSample, savePlan, solve, verify } from './api'
import { Chain } from './Chain'
import type { Report, SolveRequest } from './types'

const EMPTY: SolveRequest = { manifest: { components: [] }, current: {}, target: '', desiredVersion: '', locks: {} }

export default function App() {
  const [draft, setDraft] = useState<SolveRequest>(EMPTY)
  const [jsonText, setJsonText] = useState('')
  const [locksText, setLocksText] = useState('{}')
  const [report, setReport] = useState<Report | null>(null)
  const [savedReport, setSavedReport] = useState<Report | null>(null)
  const [independentReport, setIndependentReport] = useState<Report | null>(null)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    loadSample()
      .then((sample) => {
        setDraft(sample)
        setJsonText(JSON.stringify({ manifest: sample.manifest, current: sample.current }, null, 2))
      })
      .catch((err) => setError(String(err)))
  }, [])

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
        setIndependentReport(await verify(payload.manifest, saved.report.selection))
      }
      if (action === 'verify' && report) {
        setIndependentReport(await verify(payload.manifest, report.selection))
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

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
