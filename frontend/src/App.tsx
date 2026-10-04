import { useEffect, useMemo, useState } from 'react'
import { confirmRouteStep, listPlans, listRoutes, loadSample, loadScenarios, planRoute, savePlan, saveRoute, solve, verify } from './api'
import { Chain } from './Chain'
import type { Plan, Report, RouteOption, RouteReport, RouteStep, SavedRoute, Scenario, SolveRequest, StepCheck } from './types'

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
  const [openRoute, setOpenRoute] = useState<SavedRoute | null>(null)
  const [replanned, setReplanned] = useState<RouteReport | null>(null)
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

  async function runRoute(action: 'plan' | 'save') {
    setBusy(true)
    setError('')
    try {
      const payload = currentPayload()
      const request = {
        manifest: payload.manifest,
        current: payload.current,
        target: payload.target,
        desiredVersion: payload.desiredVersion,
        goals: payload.goals,
        locks: payload.locks,
        confirmed: confirmedSteps,
      }
      const result = action === 'save' ? (await saveRoute(request)).report : await planRoute(request)
      setRoute(result)
      if (action === 'save') await refreshRoutes()
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  // Record the per-check outcomes for the next planned step, then replan
  // from the real executed state. Recording the outcomes marks the step as
  // executed — its conversion counts even when some checks are failed or
  // 资料不足; those records stay held for backfill instead of erasing the
  // migration that already ran.
  async function confirmNextStep(step: RouteStep, checks: StepCheck[]) {
    setBusy(true)
    setError('')
    try {
      const payload = currentPayload()
      const confirmed = [...confirmedSteps, { ...step, checks, confirmed: true }]
      const request = {
        manifest: payload.manifest,
        current: payload.current,
        target: payload.target,
        desiredVersion: payload.desiredVersion,
        goals: payload.goals,
        locks: payload.locks,
        confirmed,
      }
      const result = await planRoute(request)
      setConfirmedSteps(confirmed)
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
          {confirmedSteps.length > 0 ? <button onClick={() => { setConfirmedSteps([]); setRoute(null) }} disabled={busy}>放弃已确认步骤</button> : null}
        </div>
        {route ? <RouteResult route={route} confirmedCount={confirmedSteps.length} /> : null}
        {route && route.status === 'feasible' && route.steps.length > 0 ? (
          <div className="next-confirm">
            <h3>登记第 {route.steps[0].index} 步的逐项核对结果</h3>
            <p className="hint">执行该步后逐项登记核对结论（成功 / 失败 / 资料不足），失败或资料不足可补录说明；只有全部记为成功才算真正完成，记录一旦提交不会被新路线覆盖。</p>
            <CheckEditor
              key={`next-${route.steps[0].index}-${confirmedSteps.length}`}
              checks={route.steps[0].checks ?? []}
              busy={busy}
              submitLabel="记录核对结果并确认该步"
              onSubmit={(checks) => confirmNextStep(route.steps[0], checks)}
            />
          </div>
        ) : null}
      </section>

      {comparisons.length > 0 ? <Comparison items={comparisons} /> : null}

      <SavedRoutes routes={savedRoutes} onOpen={(route) => { setOpenRoute(route); setReplanned(null) }} />

      {openRoute ? (
        <SavedRouteDetail
          route={openRoute}
          replanned={replanned}
          busy={busy}
          onConfirm={async (stepIndex, checks) => {
            setBusy(true)
            setError('')
            try {
              const updated = await confirmRouteStep({ routeId: openRoute.id, stepIndex, checks })
              setOpenRoute(updated)
              await refreshRoutes()
            } catch (err) {
              setError(err instanceof Error ? err.message : String(err))
            } finally {
              setBusy(false)
            }
          }}
          onReplan={async () => {
            setBusy(true)
            setError('')
            try {
              const result = await planRoute({ ...openRoute.request, confirmed: openRoute.report.confirmedSteps ?? [] })
              setReplanned(result)
            } catch (err) {
              setError(err instanceof Error ? err.message : String(err))
            } finally {
              setBusy(false)
            }
          }}
          onClose={() => { setOpenRoute(null); setReplanned(null) }}
        />
      ) : null}

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

function RouteResult({ route, confirmedCount }: { route: RouteReport; confirmedCount: number }) {
  return (
    <div className="route-result">
      {route.status === 'conflict' ? (
        <div className="verdict conflict card-inner">
          <p className="error-text">{route.conflictMessage}</p>
          {route.remainingConflicts?.map((check, index) => (
            <div key={index} className={'check ' + check.status}>
              <strong>{check.kind} / {check.status}</strong>
              <p>{check.message}</p>
              <Chain links={check.chain} />
            </div>
          ))}
        </div>
      ) : null}
      {route.confirmedSteps?.length ? (
        <>
          <h3>已执行步骤（历史保持原样）</h3>
          {route.confirmedSteps.map((step) => <StepCard key={'c' + step.index} step={step} />)}
        </>
      ) : null}
      {route.heldSteps?.length ? (
        <>
          <h3>已执行但核对未完成的步骤</h3>
          <p className="hint">这些步骤的转换已经执行，真实版本状态已计入；核对未全部成功只影响核验结论，可补录资料完成核对，不会重复执行转换。</p>
          {route.heldSteps.map((step) => <StepCard key={'h' + step.index} step={step} />)}
        </>
      ) : null}
      {route.options?.length ? <Options options={route.options} /> : null}
      {route.status === 'feasible' ? (
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
        {step.confirmed && !step.effective ? <em className="warning-text">（核对未全部成功，未生效）</em> : null}
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
        <ul className="compact">
          {step.checks.map((check) => (
            <li key={check.id} className={checkStatusClass(check.status)}>
              {check.label} — {checkStatusLabel(check.status)}
              {check.note ? <span className="hint">（{check.note}）</span> : null}
            </li>
          ))}
        </ul>
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

function checkStatusLabel(status: string) {
  if (status === 'success') return '成功'
  if (status === 'failed') return '失败'
  if (status === 'insufficient') return '资料不足'
  return '待确认'
}

function checkStatusClass(status: string) {
  if (status === 'success') return 'success-text'
  if (status === 'failed') return 'error-text'
  if (status === 'insufficient') return 'warning-text'
  return 'hint'
}

function Options({ options }: { options: RouteOption[] }) {
  const kindLabel: Record<string, string> = {
    continue: '保留当前状态，修正后续路线',
    backfill: '补齐核对资料（不重复执行转换）',
    rollback: '回到仍兼容的旧状态',
    stop: '停止处理',
  }
  return (
    <div className="options">
      <h3>可选方向（各自仍未解决的影响）</h3>
      <div className="compare-grid">
        {options.map((option) => (
          <div key={option.kind} className="compare-cell">
            <h3>{kindLabel[option.kind] ?? option.summary}</h3>
            <div className="selection">
              {Object.entries(option.state).sort().map(([name, version]) => <span key={name}>{name + '@' + version}</span>)}
            </div>
            <ul className="compact">
              {option.impacts.map((impact, index) => <li key={index} className="warning-text">{impact}</li>)}
            </ul>
          </div>
        ))}
      </div>
    </div>
  )
}

function SavedRouteDetail({ route, replanned, busy, onConfirm, onReplan, onClose }: {
  route: SavedRoute
  replanned: RouteReport | null
  busy: boolean
  onConfirm: (stepIndex: number, checks: StepCheck[]) => Promise<void>
  onReplan: () => Promise<void>
  onClose: () => void
}) {
  const confirmed = route.report.confirmedSteps ?? []
  const remaining = route.report.steps ?? []
  return (
    <section className="card saved">
      <h2>路线详情：{route.request.target} @ {route.request.desiredVersion}</h2>
      <p className="hint">
        已执行的转换与核对记录保持原样，不会被新路线覆盖；只有核对全部记为成功的步骤才算真正完成，
        资料不足的步骤保持待确认。保存中断后重开，每个中间状态、核对依据与剩余冲突都能独立解释。
      </p>
      <div className="actions">
        <button onClick={() => void onReplan()} disabled={busy} className="primary">按真实已完成状态重新规划</button>
        <button onClick={onClose} disabled={busy}>收起</button>
      </div>
      <div className="executed-panel">
        <h3>当前实际状态（仅已生效步骤）</h3>
        <div className="selection">
          {Object.entries(route.executedState ?? route.request.current ?? {}).sort().map(([name, version]) => <span key={name}>{name + '@' + version}</span>)}
        </div>
        {route.irreversible?.length ? (
          <>
            <h3>已发生且不可直接撤销的数据影响</h3>
            <ul className="compact">
              {route.irreversible.map((item, index) => <li key={index} className="warning-text">{item}</li>)}
            </ul>
          </>
        ) : <p className="hint">暂无不可撤销的数据影响。</p>}
      </div>
      {confirmed.length ? (
        <>
          <h3>已确认步骤（含核对记录）</h3>
          {confirmed.map((step) => (
            <div key={step.index} className={'step-card confirmed' + (step.effective ? '' : ' held')}>
              <h3>
                第 {step.index} 步
                <span className={step.effective ? 'success-text' : 'warning-text'}>{step.effective ? '已生效' : '未生效（核对未全部成功）'}</span>
              </h3>
              <ul className="compact">
                {step.changes.map((change) => (
                  <li key={change.component}>{change.component}：{change.from || '（未安装）'} → {change.to || '（退出）'}</li>
                ))}
              </ul>
              <ul className="compact">
                {(step.checks ?? []).map((check) => (
                  <li key={check.id} className={checkStatusClass(check.status)}>
                    {check.label} — {checkStatusLabel(check.status)}
                    {check.note ? <span className="hint">（{check.note}）</span> : null}
                  </li>
                ))}
              </ul>
            </div>
          ))}
        </>
      ) : null}
      {remaining.length ? (
        <>
          <h3>待执行步骤（记录核对结果后生效）</h3>
          {remaining.map((step) => (
            <PendingStep key={step.index} step={step} busy={busy} onConfirm={(checks) => onConfirm(step.index, checks)} />
          ))}
        </>
      ) : <p className="success-text">没有剩余步骤。</p>}
      {replanned ? (
        <div className="route-result">
          <h3>重新规划结果（从真实已完成状态继续）</h3>
          {replanned.status === 'conflict' ? <p className="error-text">{replanned.conflictMessage}</p> : null}
          {replanned.heldSteps?.length ? (
            <>
              <h3>未生效步骤（记录保留，不计入已完成）</h3>
              {replanned.heldSteps.map((step) => <StepCard key={'rh' + step.index} step={step} />)}
            </>
          ) : null}
          {replanned.steps.map((step) => <StepCard key={'rs' + step.index} step={step} />)}
          {replanned.options?.length ? <Options options={replanned.options} /> : null}
        </div>
      ) : null}
    </section>
  )
}

function PendingStep({ step, busy, onConfirm }: { step: RouteStep; busy: boolean; onConfirm: (checks: StepCheck[]) => Promise<void> }) {
  const checks = step.checks ?? []
  return (
    <div className="step-card">
      <h3>第 {step.index} 步</h3>
      <ul className="compact">
        {step.changes.map((change) => (
          <li key={change.component}>
            {change.component}：{change.from || '（未安装）'} → {change.to || '（退出）'}
            {step.transition?.includes(change.component) ? <em className="warning-text">（过渡版本）</em> : null}
          </li>
        ))}
      </ul>
      {step.note ? <p className="hint">{step.note}</p> : null}
      {step.rollback.irreversible?.length ? (
        <ul className="compact">
          {step.rollback.irreversible.map((item, index) => <li key={index} className="warning-text">{item}</li>)}
        </ul>
      ) : null}
      <CheckEditor checks={checks} busy={busy} submitLabel="记录核对结果（一次性，不可被新路线覆盖）" onSubmit={onConfirm} />
    </div>
  )
}

// CheckEditor registers an outcome per attached check: success, failed or
// insufficient (资料不足). Failed and insufficient outcomes accept a note so
// missing material can be supplemented later; the record itself is final.
function CheckEditor({ checks, busy, submitLabel, onSubmit }: {
  checks: StepCheck[]
  busy: boolean
  submitLabel: string
  onSubmit: (checks: StepCheck[]) => void | Promise<void>
}) {
  const [choices, setChoices] = useState<Record<string, string>>({})
  const [notes, setNotes] = useState<Record<string, string>>({})
  const allChosen = checks.length > 0 && checks.every((check) => choices[check.id])
  if (checks.length === 0) return <p className="hint">该步没有需要登记的核对项。</p>
  return (
    <div className="check-editor">
      <ul className="compact">
        {checks.map((check) => (
          <li key={check.id}>
            {check.label}
            <span className="check-actions">
              {(['success', 'failed', 'insufficient'] as const).map((status) => (
                <button
                  key={status}
                  disabled={busy}
                  className={choices[check.id] === status ? 'primary' : ''}
                  onClick={() => setChoices({ ...choices, [check.id]: status })}
                >
                  {checkStatusLabel(status)}
                </button>
              ))}
            </span>
            {(choices[check.id] === 'failed' || choices[check.id] === 'insufficient') ? (
              <input
                className="check-note"
                placeholder="补录资料 / 说明（可选）"
                value={notes[check.id] ?? ''}
                onChange={(event) => setNotes({ ...notes, [check.id]: event.target.value })}
              />
            ) : null}
          </li>
        ))}
      </ul>
      <button
        disabled={busy || !allChosen}
        className="primary"
        onClick={() => void onSubmit(checks.map((check) => ({ ...check, status: choices[check.id], note: notes[check.id]?.trim() || undefined })))}
      >
        {submitLabel}
      </button>
    </div>
  )
}

function SavedRoutes({ routes, onOpen }: { routes: SavedRoute[]; onOpen: (route: SavedRoute) => void }) {
  if (routes.length === 0) return null
  return (
    <section className="card saved">
      <h2>已保存路线（重开仍可解释）</h2>
      <p className="hint">保存时冻结请求与规划结果；重新打开后仍能看到已执行步骤、每个中间状态的独立核验与剩余选择。</p>
      <table>
        <thead><tr><th>保存时间</th><th>目标</th><th>结论</th><th>已确认</th><th>剩余步骤</th><th>最终选择</th><th></th></tr></thead>
        <tbody>
          {[...routes].reverse().map((route) => (
            <tr key={route.id}>
              <td>{route.createdAt}</td>
              <td>{route.request.target} @ {route.request.desiredVersion}</td>
              <td className={route.report.status === 'feasible' ? 'success-text' : 'error-text'}>{route.report.status}</td>
              <td>{route.report.confirmedSteps?.length ?? 0} 步</td>
              <td>{route.report.steps.length} 步</td>
              <td>{Object.entries(route.report.finalSelection ?? {}).map(([name, version]) => name + '@' + version).join(' ') || '—'}</td>
              <td><button onClick={() => onOpen(route)}>打开</button></td>
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
