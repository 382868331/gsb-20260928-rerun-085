import { useEffect, useMemo, useState } from 'react';
import { getCatalog, listPlans, savePlan, solve, verify } from './api';
import type { Catalog, Plan, Report } from './types';

const initialCurrent: Record<string, string> = {
  'web-app': '1.4.0',
  'react-ui': '18.3.1',
  'state-store': '4.2.2',
  'runtime-core': '2.2.4',
  'chart-kit': '2.3.5',
  'canvas-engine': '1.2.1'
};

function App() {
  const [catalog, setCatalog] = useState<Catalog | null>(null);
  const [target, setTarget] = useState('web-app');
  const [targetVersion, setTargetVersion] = useState('2.0.0');
  const [current] = useState(initialCurrent);
  const [locks, setLocks] = useState<Record<string, string>>({});
  const [report, setReport] = useState<Report | null>(null);
  const [savedReport, setSavedReport] = useState<Report | null>(null);
  const [savedPlan, setSavedPlan] = useState<Plan | null>(null);
  const [plans, setPlans] = useState<Plan[]>([]);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    getCatalog().then(setCatalog).catch((reason) => setError(reason.message));
    listPlans().then(setPlans).catch(() => undefined);
  }, []);

  const targetComponent = useMemo(
    () => catalog?.components.find((component) => component.name === target),
    [catalog, target]
  );

  async function calculate() {
    if (!targetComponent) return;
    setBusy(true);
    setError('');
    setSavedReport(null);
    try {
      const result = await solve({ targetComponent: target, targetVersion, current, locks });
      setReport(result);
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason));
    } finally {
      setBusy(false);
    }
  }

  async function persist() {
    if (!report?.satisfied) return;
    const plan: Plan = {
      name: `${target}@${targetVersion} 迁移方案`,
      current,
      choices: report.choices,
      target
    };
    setBusy(true);
    try {
      const response = await savePlan(plan);
      setSavedPlan(response.plan);
      setSavedReport(response.report);
      setPlans(await listPlans());
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason));
    } finally {
      setBusy(false);
    }
  }

  async function recheck() {
    if (!report) return;
    setBusy(true);
    try {
      setSavedReport(await verify({ choices: report.choices, current, target }));
    } finally {
      setBusy(false);
    }
  }

  function changeTarget(name: string) {
    setTarget(name);
    const component = catalog?.components.find((item) => item.name === name);
    if (component) {
      setTargetVersion(component.versions[component.versions.length - 1].version);
    }
    setLocks({});
  }

  function toggleLock(component: string, version: string) {
    setLocks((previous) => {
      const next = { ...previous };
      if (next[component]) delete next[component];
      else next[component] = version;
      return next;
    });
  }

  return (
    <main>
      <header>
        <div>
          <p className="eyebrow">Dependency Migration Workbench</p>
          <h1>软件依赖迁移工作台</h1>
          <p className="subtitle">先保证约束可满足，再以最少的其他组件改动得到迁移方案。</p>
        </div>
        <button onClick={calculate} disabled={busy || !catalog} className="primary">
          {busy ? '计算中…' : '计算迁移方案'}
        </button>
      </header>

      {error && <div className="error">{error}</div>}

      <section className="grid">
        <section className="card controls">
          <h2>1. 选择升级目标</h2>
          <label>组件
            <select value={target} onChange={(event) => changeTarget(event.target.value)}>
              {catalog?.components.map((component) => (
                <option key={component.name} value={component.name}>{component.name}</option>
              ))}
            </select>
          </label>
          <label>目标版本（不会自动选择最新）
            <select value={targetVersion} onChange={(event) => setTargetVersion(event.target.value)}>
              {targetComponent?.versions.map((version) => (
                <option key={version.version} value={version.version}>{version.version}</option>
              ))}
            </select>
          </label>

          <h2>2. 锁定部分选择后重算</h2>
          <div className="lock-list">
            {Object.entries(current)
              .filter(([name]) => name !== target)
              .map(([name, version]) => (
                <label key={name} className="lock-row">
                  <input
                    type="checkbox"
                    checked={Boolean(locks[name])}
                    onChange={() => toggleLock(name, version)}
                  />
                  <span>{name}</span>
                  <code>{version}</code>
                </label>
              ))}
          </div>
        </section>

        <section className="card">
          <h2>求解结论</h2>
          {!report && <p className="muted">选择目标版本后开始求解。</p>}
          {report && (
            <>
              <div className={`status ${report.satisfied ? 'ok' : 'bad'}`}>
                {report.satisfied ? '约束满足' : '约束不可满足'}
              </div>
              {report.messages.map((message) => <p key={message}>{message}</p>)}
              <div className="stats">
                <span>{Object.keys(report.choices).length} 个选择</span>
                <span>{report.changes.length} 个调整</span>
              </div>
              <ChangeTable report={report} />
              <div className="actions">
                <button onClick={recheck} disabled={busy}>独立复核</button>
                <button onClick={persist} disabled={busy || !report.satisfied} className="primary">保存并重新核验</button>
              </div>
            </>
          )}
        </section>
      </section>

      {report && (
        <section className="card wide">
          <h2>版本选择与依赖来源</h2>
          <ChoiceGrid report={report} current={current} target={target} />
        </section>
      )}

      {report?.conflicts?.length ? <Conflicts report={report} /> : null}

      {savedReport && (
        <section className="card wide">
          <h2>保存后独立核验</h2>
          <p>方案 ID：<code>{savedPlan?.id}</code></p>
          <div className={`status ${savedReport.satisfied ? 'ok' : 'bad'}`}>
            {savedReport.satisfied ? '页面结论与独立检查一致' : '独立检查发现冲突'}
          </div>
          {savedReport.messages.map((message) => <p key={message}>{message}</p>)}
        </section>
      )}

      <section className="card wide">
        <h2>已保存方案</h2>
        {plans.length === 0 ? <p className="muted">暂无方案。</p> : (
          <ul className="plans">
            {plans.map((plan) => (
              <li key={plan.id}><code>{plan.id}</code> {plan.name}</li>
            ))}
          </ul>
        )}
      </section>
    </main>
  );
}

function ChangeTable({ report }: { report: Report }) {
  if (!report.changes.length) return <p className="muted">无需调整。</p>;
  return (
    <table>
      <thead><tr><th>组件</th><th>当前</th><th>方案</th><th>原因</th></tr></thead>
      <tbody>
        {report.changes.map((change) => (
          <tr key={change.component} className={change.isTarget ? 'target' : ''}>
            <td>{change.component}{change.isOptional && <em> optional</em>}</td>
            <td>{change.from || '未选择'}</td>
            <td>{change.to}</td>
            <td>{change.reason}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

function ChoiceGrid({ report, current, target }: { report: Report; current: Record<string, string>; target: string }) {
  return (
    <div className="choices">
      {Object.entries(report.choices).sort(([a], [b]) => a.localeCompare(b)).map(([component, version]) => (
        <div key={component} className={`choice ${component === target ? 'target-choice' : ''}`}>
          <strong>{component}</strong>
          <code>{version}</code>
          <small>基线：{current[component] || '未选择'}</small>
        </div>
      ))}
    </div>
  );
}

function Conflicts({ report }: { report: Report }) {
  return (
    <section className="card wide conflicts">
      <h2>冲突依赖链</h2>
      {report.conflicts?.map((conflict, index) => (
        <article key={index}>
          <h3>{conflict.message}</h3>
          {conflict.chain.length === 0 && <p>该组件没有来自已选版本的上游依赖。</p>}
          <ol>
            {conflict.chain.map((edge, edgeIndex) => (
              <li key={edgeIndex}>
                <code>{edge.from}@{edge.fromVersion}</code>
                {' → '}
                <code>{edge.to}</code>
                要求 <code>{edge.constraint}</code>
                {edge.optional && <em>（可选依赖）</em>}
              </li>
            ))}
          </ol>
        </article>
      ))}
    </section>
  );
}

export default App;
