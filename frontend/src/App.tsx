import { useEffect, useMemo, useState } from 'react'
import { advanceRoute, listPlans, listRoutes, loadSample, loadScenarios, planRoute, savePlan, saveRoute, solve, verify } from './api'
import { Chain } from './Chain'
import type { Plan, Report, Route, RoutePlan, Scenario, SolveRequest } from './types'

const EMPTY: SolveRequest = { manifest: { components: [] }, current: {}, target: '', desiredVersion: '', locks: {} }

export default function App() {
  const [scenarios, setScenarios] = useState<Scenario[]>([])
  const [scenarioID, setScenarioID] = useState('base')
  const [draft, setDraft] = useState<SolveRequest>(EMPTY)
  const [jsonText, setJsonText] = useState('')
  const [locksText, setLocksText] = useState('{}')
  const [report, setReport] = useState<Report | null>(null)
  const [savedReport, setSavedReport] = useState<Report | null>(null)
  const [independentReport, setIndependentReport] = useState<Report | null>(null)
  const [comparisons, setComparisons] = useState<{ label: string; locks: Record<string, string>; report: Report }[]>([])
  const [plans, setPlans] = useState<Plan[]>([])
  const [routes, setRoutes] = useState<Route[]>([])
  const [routePreview, setRoutePreview] = useState<RoutePlan | null>(null)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    loadScenarios()
      .then((items) => {
        setScenarios(items)
        return applyScenario(items[0]?.id ?? 'base')
      })
      .then(() => refreshPlans())
      .then(() => refreshRoutes())
      .catch((err) => setError(String(err)))
  }, [])

  async function applyScenario(id: string) {
    const scenario = await loadSample(id)
    setScenarioID(id)
    setDraft(scenario.request)
    setJsonText(JSON.stringify({ manifest: scenario.request.manifest, current: scenario.request.current }, null, 2))
    setLocksText(JSON.stringify(scenario.request.locks ?? {}, null, 2))
    setReport(null)
    setSavedReport(null)
    setIndependentReport(null)
    setComparisons([])
    setRoutePreview(null)
    setError('')
  }

  function refreshPlans() {
    return listPlans().then(setPlans).catch((err) => setError(String(err)))
  }

  function refreshRoutes() {
    return listRoutes().then(setRoutes).catch((err) => setError(String(err)))
  }

  const targetVersions = useMemo(() => {
    return draft.manifest.components.find((item) => item.name === draft.target)?.versions.map((item) => item.version) ?? []
  }, [draft.manifest, draft.target])

  function currentPayload(): SolveRequest {
    const parsed = JSON.parse(jsonText) as Pick<SolveRequest, 'manifest' | 'current'>
    return {
      ...draft,
      ...parsed,
      locks: JSON.parse(locksText || '{}') as Record<string, string>,
    }
  }

  async function run(action: 'solve' | 'save' | 'verify' | 'compare' | 'plan-route' | 'save-route') {
    setBusy(true)
    setError('')
    try {
      const payload = currentPayload()
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
        setIndependentReport(saved.verification)
        await refreshPlans()
      }
      if (action === 'verify' && report) {
        setIndependentReport(await verify(payload, report.selection))
      }
      if (action === 'compare') {
        const result = await solve(payload)
        const label = `锁定 ${Object.entries(payload.locks ?? {}).map(([name, version]) => `${name}@${version}`).join('、') || '无锁定'}`
        setComparisons((items) => [...items.filter((item) => item.label !== label), { label, locks: payload.locks ?? {}, report: result }])
      }
      if (action === 'plan-route') {
        setRoutePreview(await planRoute(payload))
      }
      if (action === 'save-route') {
        await saveRoute(payload)
        setRoutePreview(null)
        await refreshRoutes()
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
        <button onClick={() => void run('save')} disabled={busy} className="primary">保存并独立核验</button>
      </header>

      {error ? <div className="error">{error}</div> : null}

      <section className="card scenarios">
        <h2>验证资料</h2>
        <p className="hint">内置常规升级、循环依赖、锁定冲突、缺失候选、过渡版本、相互依赖、可选退出、中途改目标等资料；切换会清空当前页面结论，但不会改动已保存方案与路线。</p>
        <div className="scenario-row">
          <select value={scenarioID} onChange={(event) => void applyScenario(event.target.value)}>
            {scenarios.map((item) => <option key={item.id} value={item.id}>{item.name}</option>)}
          </select>
          <span className="hint">{scenarios.find((item) => item.id === scenarioID)?.description}</span>
        </div>
      </section>

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
            <button onClick={() => void run('compare')} disabled={busy}>用当前锁定加入对比</button>
            <button onClick={() => void run('verify')} disabled={busy || !report}>独立核验当前选择</button>
            <button onClick={() => void run('plan-route')} disabled={busy}>规划分阶段路线</button>
            <button onClick={() => void run('save-route')} disabled={busy}>保存为新路线</button>
          </div>
        </div>
      </section>

      {routePreview ? <RoutePlanPreview plan={routePreview} /> : null}

      <SavedRoutes routes={routes} onChanged={refreshRoutes} />

      {report ? <Result report={report} saved={savedReport} independent={independentReport} /> : null}

      {comparisons.length > 0 ? <Comparison items={comparisons} /> : null}

      <SavedPlans
        plans={plans}
        onReverify={async (plan) => {
          const result = await verify(plan.request, plan.report.selection)
          setPlans((items) => items.map((item) => item.id === plan.id ? { ...item, verification: result, consistent: plan.report.status === result.status } : item))
          return result
        }}
      />
    </main>
  )
}

function Comparison({ items }: { items: { label: string; locks: Record<string, string>; report: Report }[] }) {
  return (
    <section className="card compare">
      <h2>迁移方案对比</h2>
      <p className="hint">修改锁定组件后分别加入对比；实际改动、最终选择与冲突依赖链都会保留，不会被后续重算覆盖。</p>
      <div className="compare-grid">
        {items.map((item) => (
          <div key={item.label} className={'compare-cell ' + item.report.status}>
            <h3>{item.label}</h3>
            <p className={item.report.status === 'feasible' ? 'success-text' : 'error-text'}>
              {item.report.status === 'feasible' ? `可满足 · 调整 ${item.report.changedCount} 个组件` : '存在冲突'}
            </p>
            {item.report.conflictMessage ? <p>{item.report.conflictMessage}</p> : null}
            <Chain links={item.report.conflictChain} />
            <ul className="compact">
              {item.report.changes.map((change) => (
                <li key={change.component}>{change.component}：{change.from} → {change.to}<span className="hint">（{change.reason}）</span></li>
              ))}
            </ul>
            <div className="selection">
              {Object.entries(item.report.selection).sort().map(([name, version]) => <span key={name}>{name + '@' + version}</span>)}
            </div>
            {item.report.optionalOmitted?.length ? <p className="warning-text">跳过可选：{item.report.optionalOmitted.join('、')}</p> : null}
          </div>
        ))}
      </div>
    </section>
  )
}

function SavedPlans({ plans, onReverify }: { plans: Plan[]; onReverify: (plan: Plan) => Promise<Report> }) {
  const [busyID, setBusyID] = useState('')
  return (
    <section className="card saved">
      <h2>已保存方案（独立快照）</h2>
      <p className="hint">保存时会冻结请求的 manifest 副本并用独立 verifier 重新核验；之后修改页面约束，旧方案仍保留原版本与原结论。</p>
      {plans.length === 0 ? <p className="hint">还没有保存方案。</p> : (
        <table>
          <thead><tr><th>保存时间</th><th>目标</th><th>求解结论</th><th>独立核验</th><th>选择</th><th></th></tr></thead>
          <tbody>
            {[...plans].reverse().map((plan) => (
              <tr key={plan.id}>
                <td>{plan.createdAt}</td>
                <td>{plan.request.target} @ {plan.request.desiredVersion}</td>
                <td className={plan.report.status === 'feasible' ? 'success-text' : 'error-text'}>{plan.report.status}</td>
                <td className={plan.verification.status === 'feasible' ? 'success-text' : 'error-text'}>
                  {plan.verification.status}
                  {plan.consistent ? '' : '（不一致）'}
                </td>
                <td>{Object.entries(plan.report.selection).map(([name, version]) => `${name}@${version}`).join(' ') || '—'}</td>
                <td>
                  <button disabled={busyID === plan.id} onClick={() => {
                    setBusyID(plan.id)
                    onReverify(plan).finally(() => setBusyID(''))
                  }}>用保存时快照再核验</button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </section>
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

      {report.goalStatuses?.length ? (
        <div className="card">
          <h2>迁移目标覆盖</h2>
          <table>
            <thead><tr><th>目标</th><th>要求</th><th>选中</th><th>状态</th><th>原因</th></tr></thead>
            <tbody>
              {report.goalStatuses.map((goal) => (
                <tr key={goal.component + '@' + goal.required}>
                  <td>{goal.component}</td>
                  <td>{goal.required}</td>
                  <td>{goal.selected || '—'}</td>
                  <td className={goal.status === 'satisfied' ? 'success-text' : 'error-text'}>{goal.status}</td>
                  <td>{goal.reason || '—'}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : null}

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

function RoutePlanPreview({ plan }: { plan: RoutePlan }) {
  return (
    <section className="card">
      <h2>分阶段路线预览（未保存）</h2>
      {plan.status !== 'feasible' ? (
        <div>
          <p className="error-text">找不到可执行路线：{plan.conflictMessage}</p>
          <Chain links={plan.conflictChain} />
        </div>
      ) : (
        <p className="hint">共 {plan.steps.length} 步；每一步落地后的状态都经过独立核验。保存路线后可逐步确认执行。</p>
      )}
      <StepList steps={plan.steps} confirmed={0} />
    </section>
  )
}

function StepList({ steps, confirmed }: { steps: Route['steps']; confirmed: number }) {
  if (steps.length === 0) return null
  return (
    <ol className="steps">
      {steps.map((step) => (
        <li key={step.index} className={'step' + (step.index <= confirmed ? ' confirmed' : '')}>
          <div>
            <strong>第 {step.index} 步</strong>
            {step.index <= confirmed ? <span className="badge">已确认</span> : null}
            {step.transitional ? <span className="badge warn">过渡版本</span> : null}
            {step.together ? <span className="badge warn">一起切换</span> : null}
            <span className={step.verification.status === 'feasible' ? 'success-text' : 'error-text'}>
              独立核验：{step.verification.status}
            </span>
          </div>
          <ul className="compact">
            {step.changes.map((change) => (
              <li key={change.component}>{change.component}：{change.from || '未安装'} → {change.to}<span className="hint">（{change.reason}）</span></li>
            ))}
          </ul>
          {step.reason ? <p className="hint">{step.reason}</p> : null}
          {step.verification.checks.filter((check) => check.status === 'warning').map((check, index) => (
            <p key={index} className="warning-text">{check.message}</p>
          ))}
        </li>
      ))}
    </ol>
  )
}

function SavedRoutes({ routes, onChanged }: { routes: Route[]; onChanged: () => Promise<void> }) {
  return (
    <section className="card">
      <h2>分阶段迁移路线</h2>
      <p className="hint">已确认的步骤是冻结历史：修改最终目标或锁定未升级组件后，只从已确认状态继续规划，历史步骤不会被重算。保存重开后仍保留每步状态与独立核验。</p>
      {routes.length === 0 ? <p className="hint">还没有保存的路线。</p> : routes.map((route) => (
        <RouteCard key={route.id} route={route} onChanged={onChanged} />
      ))}
    </section>
  )
}

function RouteCard({ route, onChanged }: { route: Route; onChanged: () => Promise<void> }) {
  const [target, setTarget] = useState(route.request.target)
  const [version, setVersion] = useState('')
  const [locksText, setLocksText] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  async function advance(payload: { confirm: number; changeGoal: boolean }) {
    setBusy(true)
    setError('')
    try {
      const body: Parameters<typeof advanceRoute>[0] = { id: route.id, confirm: payload.confirm }
      if (payload.changeGoal) {
        if (version) {
          body.target = target
          body.desiredVersion = version
        }
        if (locksText.trim()) {
          body.locks = JSON.parse(locksText) as Record<string, string>
        }
      }
      await advanceRoute(body)
      await onChanged()
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  const remaining = route.steps.length - route.confirmed
  return (
    <div className={'route-card ' + route.status}>
      <h3>
        {route.request.target} → {route.request.desiredVersion}
        <span className={route.status === 'feasible' ? 'success-text' : 'error-text'}>（{route.status === 'feasible' ? '路线可执行' : '路线受阻'}）</span>
      </h3>
      <p className="hint">已确认 {route.confirmed} / {route.steps.length} 步 · 更新于 {route.updatedAt}</p>
      {error ? <p className="error-text">{error}</p> : null}
      {route.status === 'blocked' ? (
        <div className="error-box">
          <p className="error-text">剩余冲突：{route.conflictMessage}</p>
          <Chain links={route.conflictChain} />
          {route.rollback?.length ? (
            <div>
              <p className="hint">可回退到的兼容状态：</p>
              <ul className="compact">
                {route.rollback.map((point) => (
                  <li key={point.step}>
                    第 {point.step} 步之后：{Object.entries(point.state).map(([name, v]) => `${name}@${v}`).join(' ')}
                    {point.revertible
                      ? <span className="success-text">（可直接撤回）</span>
                      : <span className="warning-text">（不可直接撤回：{point.reason}）</span>}
                  </li>
                ))}
              </ul>
            </div>
          ) : null}
        </div>
      ) : null}
      <StepList steps={route.steps} confirmed={route.confirmed} />
      {route.revisions?.length ? (
        <div>
          <p className="hint">目标变更记录：</p>
          <ul className="compact">
            {route.revisions.map((revision, index) => (
              <li key={index}>{revision.at}：确认 {revision.confirmedSteps} 步后调整目标/锁定，{revision.note}</li>
            ))}
          </ul>
        </div>
      ) : null}
      <div className="actions">
        <button disabled={busy || remaining <= 0} onClick={() => void advance({ confirm: 1, changeGoal: false })}>确认下一步（剩余 {remaining}）</button>
        <label>改目标
          <input value={target} onChange={(event) => setTarget(event.target.value)} placeholder="组件" />
        </label>
        <label>版本
          <input value={version} onChange={(event) => setVersion(event.target.value)} placeholder="如 2.1.0" />
        </label>
        <label>锁定（JSON）
          <input value={locksText} onChange={(event) => setLocksText(event.target.value)} placeholder='{"db":"1.0.0"}' />
        </label>
        <button disabled={busy || (!version && !locksText.trim())} onClick={() => void advance({ confirm: 0, changeGoal: true })}>改目标/锁定并继续规划</button>
      </div>
    </div>
  )
}
