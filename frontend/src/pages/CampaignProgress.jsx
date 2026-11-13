import { useEffect, useState } from 'react'
import { api } from '../api.js'

// Where the campaign is in the loop: create -> plan -> draft -> submit -> approve -> leads -> hand off -> close.
// Data comes from GET /campaign-progress (counts only, no personal data).

export function useProgressMap(version) {
  const [map, setMap] = useState({})
  useEffect(() => { api('GET', '/campaign-progress').then(setMap).catch(() => setMap({})) }, [version])
  return map
}

export function ProgressCell({ data }) {
  if (!data) return <span className="muted">-</span>
  const cur = data.steps[data.current]
  return (
    <span className="prog-cell">
      <span className="prog-bar" aria-hidden="true">
        {data.steps.map((s) => <i key={s.key} className={s.done ? 'on' : ''} />)}
      </span>
      <span className="small">{cur.label}</span>
      {data.note && <span className="prog-note">{data.note}</span>}
    </span>
  )
}

export default function CampaignProgress({ id, version }) {
  const [data, setData] = useState(null)
  useEffect(() => { api('GET', `/campaign-progress/${id}`).then(setData).catch(() => setData(null)) }, [id, version])
  if (!data) return null
  return (
    <section className="block prog" aria-label="ความคืบหน้าแคมเปญ">
      <ol className="prog-steps">
        {data.steps.map((s, i) => (
          <li key={s.key} className={`${s.done ? 'done' : ''} ${i === data.current ? 'current' : ''}`} aria-current={i === data.current ? 'step' : undefined}>
            <span className="prog-dot">{s.done ? '✓' : i + 1}</span>
            <span className="prog-label">{s.label}</span>
            {s.detail && <span className="prog-detail">{s.detail}</span>}
          </li>
        ))}
      </ol>
      {data.note && <p className="prog-note-line">ต้องทำต่อ: {data.note}</p>}
    </section>
  )
}
