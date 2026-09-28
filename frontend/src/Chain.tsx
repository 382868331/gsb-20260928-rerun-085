import type { ChainLink } from './types'

export function Chain({ links }: { links?: ChainLink[] }) {
  if (!links?.length) return null
  return (
    <ol className="chain">
      {links.map((link, index) => (
        <li key={link.from + '-' + link.to + '-' + index}>
          <strong>{link.from}</strong>
          {link.fromVersion ? <span> {link.fromVersion}</span> : null}
          <span className="arrow"> → </span>
          <strong>{link.to}</strong>
          <span> 要求 {link.constraint}</span>
          {link.optional ? <em>（可选）</em> : null}
        </li>
      ))}
    </ol>
  )
}
