import type { Plan } from './types'

type Row = { component: string; a?: string; b?: string }

function diffRows(a: Plan, b: Plan): Row[] {
  const names = new Set<string>([...Object.keys(a.report.selection), ...Object.keys(b.report.selection)])
  return [...names]
    .sort()
    .map((component) => ({ component, a: a.report.selection[component], b: b.report.selection[component] }))
}

export function Compare({ left, right }: { left: Plan | null; right: Plan | null }) {
  if (!left || !right) {
    return <p className="hint">选择两个已保存方案后显示实际改动差异。</p>
  }
  const rows = diffRows(left, right)
  return (
    <div>
      <table>
        <thead>
          <tr><th>组件</th><th>方案 {left.id.slice(0, 6)}（{left.report.status}）</th><th>方案 {right.id.slice(0, 6)}（{right.report.status}）</th></tr>
        </thead>
        <tbody>
          {rows.map((row) => (
            <tr key={row.component} className={row.a === row.b ? '' : 'diff'}>
              <td>{row.component}</td>
              <td>{row.a ?? '—'}</td>
              <td>{row.b ?? '—'}</td>
            </tr>
          ))}
        </tbody>
      </table>
      {left.report.status === 'conflict' || right.report.status === 'conflict' ? (
        <div className="conflict-compare">
          {[left, right].map((plan) => (
            <div key={plan.id} className={'check ' + (plan.report.status === 'conflict' ? 'error' : '')}>
              <strong>方案 {plan.id.slice(0, 6)} 冲突链</strong>
              <p>{plan.report.conflictMessage}</p>
              <ol className="chain">
                {(plan.report.conflictChain ?? []).map((link, index) => (
                  <li key={index}>
                    <strong>{link.from}</strong>
                    {link.fromVersion ? <span> {link.fromVersion}</span> : null}
                    <span className="arrow"> → </span>
                    <strong>{link.to}</strong>
                    <span> 要求 {link.constraint}</span>
                  </li>
                ))}
              </ol>
            </div>
          ))}
        </div>
      ) : null}
    </div>
  )
}
