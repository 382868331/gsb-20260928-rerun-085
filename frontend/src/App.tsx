import { useEffect, useMemo, useState } from 'react'
import { confirmRouteStep, listPlans, listRoutes, loadSample, loadScenarios, planRoute, savePlan, saveRoute, solve, verify } from './api'
import { Chain } from './Chain'
import type { Plan, Report, RouteReport, RouteStep, SavedRoute, Scenario, SolveRequest } from './types'

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
  const [confirmedSteps, setConfirmedSteps] = useState<RouteStep[]>([])
  const [route, setRoute] = useState<RouteReport | null>(null)
  const [savedRoutes, setSavedRoutes] = useState<SavedRoute[]>([])
  const [checkDraft, setCheckDraft] = useState<Record<string, string>>({})
  const [savedRouteId, setSavedRouteId] = useState<string | null>(null)
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
    setConfirmedSteps([])
    setRoute(null)
    setCheckDraft({})
    setSavedRouteId(null)
    setError('')
  }

  function refreshPlans() {
    return listPlans().then(setPlans).catch((err) => setError(String(err)))
  }

  function refreshRoutes() {
    return listRoutes().then(setSavedRoutes).catch((err) => setError(String(err)))
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

  async function run(action: 'solve' | 'save' | 'verify' | 'compare') {
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
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  function routeRequestPayload(confirmed: RouteStep[]) {
    const payload = currentPayload()
    return {
      manifest: payload.manifest,
      current: payload.current,
      target: payload.target,
      desiredVersion: payload.desiredVersion,
      goals: payload.goals,
      locks: payload.locks,
      confirmed,
    }
  }

  async function runRoute(action: 'plan' | 'save') {
    setBusy(true)
    setError('')
    try {
      const request = routeRequestPayload(confirmedSteps)
      if (action === 'save') {
        const saved = await saveRoute(request)
        setSavedRouteId(saved.id)
        setRoute(saved.report)
        await refreshRoutes()
        return
      }
      const result = await planRoute(request)
      setRoute(result)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  // Record the operator's check outcomes for the next planned step. Only a
  // step whose checks were all recorded as success counts as completed;
  // failure blocks the route with options, insufficient data keeps it pending.
  async function confirmNext() {
    if (!route || route.steps.length === 0) return
    setBusy(true)
    setError('')
    try {
      const step = route.steps[0]
      const records = (step.checks ?? []).map((check) => ({
        name: check.name,
        result: checkDraft[check.name] ?? 'insufficient-data',
      }))
      if (savedRouteId) {
        const updated = await confirmRouteStep(savedRouteId, { stepIndex: step.index, records })
        setConfirmedSteps(updated.report.confirmedSteps ?? [])
        setRoute(updated.report)
        await refreshRoutes()
      } else {
        const confirmed = [...confirmedSteps, { ...step, executed: true, records }]
        const result = await planRoute(routeRequestPayload(confirmed))
        setConfirmedSteps(confirmed)
        setRoute(result)
      }
      setCheckDraft({})
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  // A blocked route offers three ways forward; executed conversions and
  // their check records are never rewritten, only carried over.
  async function chooseOption(kind: string) {
    if (!route) return
    if (kind === 'stop') return
    setBusy(true)
    setError('')
    try {
      let confirmed = confirmedSteps
      if (kind === 'continue') {
        const last = confirmed[confirmed.length - 1]
        confirmed = [...confirmed.slice(0, -1), { ...last, acceptedFailure: true }]
      }
      if (kind === 'rollback') {
        confirmed = confirmed.slice(0, -1)
      }
      const result = await planRoute(routeRequestPayload(confirmed))
      setConfirmedSteps(confirmed)
      setSavedRouteId(null)
      setRoute(result)
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
        <p className="hint">内置正常升级、循环依赖、锁定版本冲突、不存在候选四类资料；切换会清空当前页面结论，但不会改动已保存方案。</p>
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
          </div>
        </div>
      </section>

      {report ? <Result report={report} saved={savedReport} independent={independentReport} /> : null}

      <section className="card route-panel">
        <h2>分阶段升级路线</h2>
        <p className="hint">
          每个中间状态都经过独立核验；单步不允许跨多个大版本，必要时会安排过渡版本。
          {confirmedSteps.length > 0 ? '已确认的步骤保持原样，可修改目标或锁定后从已完成状态继续规划。' : '确认前几步后，可修改最终目标或锁定未升级组件，再从已完成状态继续。'}
        </p>
        <div className="actions">
          <button onClick={() => void runRoute('plan')} disabled={busy} className="primary">{confirmedSteps.length > 0 ? '从已完成状态继续规划' : '规划分阶段路线'}</button>
          <button onClick={() => void runRoute('save')} disabled={busy || !route}>保存路线</button>
          {confirmedSteps.length > 0 ? <button onClick={() => { setConfirmedSteps([]); setRoute(null); setSavedRouteId(null) }} disabled={busy}>放弃已确认步骤</button> : null}
        </div>
        {route && route.status === 'feasible' && route.steps.length > 0 ? (
          <div className="card-inner">
            <h3>第 {route.steps[0].index} 步执行后核对</h3>
            <p className="hint">逐项记录实际核对结果；全部成功才算该步完成，资料不足会保持待确认，失败会给出可选处理方式。</p>
            {(route.steps[0].checks ?? []).map((check) => (
              <div key={check.name} className="check-row">
                <div>
                  <strong>{check.name}</strong>
                  <p className="hint">{check.expect}</p>
                </div>
                <select
                  value={checkDraft[check.name] ?? ''}
                  onChange={(event) => setCheckDraft({ ...checkDraft, [check.name]: event.target.value })}
                >
                  <option value="">待记录</option>
                  <option value="success">成功</option>
                  <option value="failure">失败</option>
                  <option value="insufficient-data">资料不足</option>
                </select>
              </div>
            ))}
            <button onClick={() => void confirmNext()} disabled={busy} className="primary">记录核对结果并确认该步</button>
          </div>
        ) : null}
        {route ? <RouteResult route={route} confirmedCount={confirmedSteps.length} onOption={(kind) => void chooseOption(kind)} /> : null}
      </section>

      {comparisons.length > 0 ? <Comparison items={comparisons} /> : null}

      <SavedRoutes routes={savedRoutes} onLoad={(saved) => {
        setConfirmedSteps(saved.report.confirmedSteps ?? [])
        setRoute(saved.report)
        setSavedRouteId(saved.id)
        setCheckDraft({})
      }} />

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

function RouteResult({ route, confirmedCount, onOption }: { route: RouteReport; confirmedCount: number; onOption: (kind: string) => void }) {
  return (
    <div className="route-result">
      {route.status === 'conflict' || route.status === 'blocked' || route.status === 'pending' ? (
        <div className="verdict conflict card-inner">
          <p className="error-text">{route.conflictMessage}</p>
          {route.remainingConflicts?.map((check, index) => (
            <div key={index} className={'check ' + check.status}>
              <strong>{check.kind} / {check.status}</strong>
              <p>{check.message}</p>
              <Chain links={check.chain} />
            </div>
          ))}
          {route.pendingStep ? <p className="warning-text">第 {route.pendingStep.index} 步资料不足，保持待确认；以下剩余步骤从其之前的状态重新规划。</p> : null}
          {route.options?.length ? (
            <div className="compare-grid">
              {route.options.map((option) => (
                <div key={option.kind} className="compare-cell conflict">
                  <h3>{option.title}</h3>
                  {option.state ? (
                    <div className="selection">
                      {Object.entries(option.state).sort().map(([name, version]) => <span key={name}>{name + '@' + version}</span>)}
                    </div>
                  ) : null}
                  <p className="hint">仍未解决的影响：</p>
                  <ul className="compact">
                    {option.unresolved?.map((item, index) => <li key={index} className="warning-text">{item}</li>)}
                  </ul>
                  {option.kind !== 'stop' ? <button onClick={() => onOption(option.kind)}>选择此方式</button> : <p className="hint">保持现状，不做任何改动。</p>}
                </div>
              ))}
            </div>
          ) : null}
        </div>
      ) : null}
      {route.confirmedSteps?.length ? (
        <>
          <h3>已执行步骤（历史保持原样）</h3>
          {route.confirmedSteps.map((step) => <StepCard key={'c' + step.index} step={step} />)}
        </>
      ) : null}
      {route.status === 'feasible' || route.status === 'pending' ? (
        route.steps.length === 0 ? (
          <p className="success-text">目标已达成，没有剩余步骤。</p>
        ) : (
          <>
            <h3>{confirmedCount > 0 ? '剩余步骤（从已完成状态继续）' : '待执行步骤'}</h3>
            {route.steps.map((step) => <StepCard key={step.index} step={step} />)}
          </>
        )
      ) : route.steps.length > 0 ? (
        <>
          <h3>无法继续前已排定的步骤</h3>
          {route.steps.map((step) => <StepCard key={step.index} step={step} />)}
        </>
      ) : null}
    </div>
  )
}

function StepCard({ step }: { step: RouteStep }) {
  return (
    <div className={'step-card' + (step.confirmed ? ' confirmed' : '')}>
      <h3>
        第 {step.index} 步{step.confirmed ? '（已确认）' : ''}
        <span className={step.verification.status === 'feasible' ? 'success-text' : 'error-text'}> 独立核验：{step.verification.status}</span>
      </h3>
      <ul className="compact">
        {step.changes.map((change) => (
          <li key={change.component}>
            {change.component}：{change.from || '（未安装）'} → {change.to || '（退出）'}
            {step.transition?.includes(change.component) ? <em className="warning-text">（过渡版本）</em> : null}
          </li>
        ))}
      </ul>
      {step.together?.length ? <p className="hint">必须一起切换：{step.together.join('、')}</p> : null}
      {step.note ? <p className="hint">{step.note}</p> : null}
      {step.checks?.length ? (
        <details open={!!step.records?.length}>
          <summary>执行后核对（{step.records?.length ? '已记录' : '待记录'}）</summary>
          <ul className="compact">
            {step.checks.map((check) => {
              const record = step.records?.find((item) => item.name === check.name)
              return (
                <li key={check.name}>
                  {check.name}：{check.expect}
                  {record ? (
                    <span className={record.result === 'success' ? 'success-text' : record.result === 'failure' ? 'error-text' : 'warning-text'}>
                      {' '}— {record.result === 'success' ? '成功' : record.result === 'failure' ? '失败' : '资料不足'}
                    </span>
                  ) : <span className="hint"> — 待记录</span>}
                </li>
              )
            })}
          </ul>
          {step.acceptedFailure ? <p className="warning-text">核对失败已被操作员明确接受，路线在此状态下继续。</p> : null}
        </details>
      ) : null}
      <details>
        <summary>回退说明</summary>
        <p className="hint">可回退到的兼容状态：</p>
        <div className="selection">
          {Object.entries(step.rollback.state).sort().map(([name, version]) => <span key={name}>{name + '@' + version}</span>)}
        </div>
        {step.rollback.irreversible?.length ? (
          <ul className="compact">
            {step.rollback.irreversible.map((item, index) => <li key={index} className="warning-text">{item}</li>)}
          </ul>
        ) : <p className="hint">本步没有不可直接撤回的影响。</p>}
      </details>
    </div>
  )
}

function SavedRoutes({ routes, onLoad }: { routes: SavedRoute[]; onLoad: (route: SavedRoute) => void }) {
  if (routes.length === 0) return null
  return (
    <section className="card saved">
      <h2>已保存路线（重开仍可解释）</h2>
      <p className="hint">保存时冻结请求与规划结果；重新打开后仍能看到已执行步骤、每步核对记录、每个中间状态的独立核验与剩余选择。已执行的转换与核对记录不会被新路线覆盖。</p>
      <table>
        <thead><tr><th>保存时间</th><th>目标</th><th>结论</th><th>已执行</th><th>剩余步骤</th><th>最终选择</th><th></th></tr></thead>
        <tbody>
          {[...routes].reverse().map((route) => (
            <tr key={route.id}>
              <td>{route.createdAt}</td>
              <td>{route.request.target} @ {route.request.desiredVersion}</td>
              <td className={route.report.status === 'feasible' ? 'success-text' : 'error-text'}>{route.report.status}</td>
              <td>{route.report.confirmedSteps?.length ?? 0} 步</td>
              <td>{route.report.steps.length} 步</td>
              <td>{Object.entries(route.report.finalSelection ?? {}).map(([name, version]) => name + '@' + version).join(' ') || '—'}</td>
              <td><button onClick={() => onLoad(route)}>载入继续核对</button></td>
            </tr>
          ))}
        </tbody>
      </table>
    </section>
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
