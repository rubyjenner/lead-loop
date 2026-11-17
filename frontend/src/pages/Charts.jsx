import { useState } from 'react'
import { translate, useLang } from '../i18n.jsx'

// Small SVG/HTML charts for the report page (no chart library). Colors come from CSS variables in styles.css
// (--series-1..3, validated light and dark). Every chart has a hover/touch tooltip, a legend when there are 2+
// series, and the same numbers are in the tables below it, so nothing depends on color alone.

function useTr() {
  const { lang } = useLang()
  return (x) => (lang === 'en' ? translate(x) : x)
}

function useTip() {
  const [tip, setTip] = useState(null)
  return {
    tip,
    show: (e, lines) => {
      const box = e.currentTarget.closest('.viz-wrap').getBoundingClientRect()
      const pt = e.touches ? e.touches[0] : e
      setTip({ x: Math.min(Math.max(pt.clientX - box.left, 60), box.width - 60), y: Math.max(pt.clientY - box.top - 12, 10), lines })
    },
    hide: () => setTip(null),
  }
}

function Tip({ tip }) {
  if (!tip) return null
  return (
    <div className="viz-tip" style={{ left: tip.x, top: tip.y }} role="status">
      {tip.lines.map((l, i) => <div key={i} className={i === 0 ? 'viz-tip-h' : ''}>{l}</div>)}
    </div>
  )
}

export function Legend({ series }) {
  if (series.length < 2) return null
  return (
    <ul className="viz-legend" aria-label="คำอธิบายสี">
      {series.map((s) => <li key={s.key}><i style={{ background: `var(--series-${s.slot})` }} />{s.label}</li>)}
    </ul>
  )
}

// rows: [{label, parts: {key: number}}]; series: [{key, label, slot}]; one bar per row, same scale for all rows.
export function StackedBars({ rows, series, unit = 'ลีด', empty = 'ยังไม่มีข้อมูล' }) {
  const t = useTip()
  const tr = useTr()
  unit = tr(unit)
  if (!rows.length) return <p className="muted small">{empty}</p>
  const total = (r) => series.reduce((n, s) => n + (r.parts[s.key] || 0), 0)
  const max = Math.max(1, ...rows.map(total))
  return (
    <div className="viz-wrap">
      <Legend series={series} />
      <ul className="viz-bars">
        {rows.map((r) => {
          const lastIdx = series.map((s, i) => (r.parts[s.key] || 0) > 0 ? i : -1).filter((i) => i >= 0).pop()
          return (
            <li key={r.label}>
              <span className="viz-label" title={r.label}>{r.label}</span>
              <span className="viz-track">
                {series.map((s, i) => {
                  const v = r.parts[s.key] || 0
                  if (!v) return null
                  return (
                    <span key={s.key} className="viz-seg" tabIndex={0}
                      style={{ width: `${(v / max) * 100}%`, background: `var(--series-${s.slot})`, borderRadius: i === lastIdx ? '0 4px 4px 0' : 0 }}
                      onMouseMove={(e) => t.show(e, [r.label, `${s.label}: ${v} ${unit}`, `${tr('รวม')} ${total(r)} ${unit}`])}
                      onTouchStart={(e) => t.show(e, [r.label, `${s.label}: ${v} ${unit}`, `${tr('รวม')} ${total(r)} ${unit}`])}
                      onMouseLeave={t.hide} onBlur={t.hide} />
                  )
                })}
              </span>
              <b className="viz-total">{total(r)}</b>
            </li>
          )
        })}
      </ul>
      <Tip tip={t.tip} />
    </div>
  )
}

// points: [{label:'14/11', value:3}]  single series, so no legend; the title names it.
export function LineChart({ points, unit = 'ลีด' }) {
  const t = useTip()
  const tr = useTr()
  unit = tr(unit)
  const [hover, setHover] = useState(-1)
  if (!points.length) return <p className="muted small">ยังไม่มีข้อมูล</p>
  const W = 640, H = 210, L = 30, R = 12, T = 10, B = 28
  const maxV = Math.max(1, ...points.map((p) => p.value))
  const top = maxV <= 4 ? 4 : Math.ceil(maxV / 4) * 4
  const x = (i) => L + (points.length === 1 ? 0 : (i / (points.length - 1)) * (W - L - R))
  const y = (v) => T + (1 - v / top) * (H - T - B)
  const d = points.map((p, i) => `${i ? 'L' : 'M'}${x(i).toFixed(1)},${y(p.value).toFixed(1)}`).join(' ')
  const ticks = [0, top / 2, top]
  const labelIdx = new Set([0, Math.floor((points.length - 1) / 2), points.length - 1])
  const sum = points.reduce((n, p) => n + p.value, 0)
  return (
    <div className="viz-wrap">
      <svg viewBox={`0 0 ${W} ${H}`} className="viz-svg" role="img" aria-label={`ลีดใหม่ ${points.length} วันล่าสุด รวม ${sum} ${unit}`}>
        {ticks.map((v) => (
          <g key={v}>
            <line x1={L} x2={W - R} y1={y(v)} y2={y(v)} className="viz-grid" />
            <text x={L - 6} y={y(v) + 4} textAnchor="end" className="viz-axis">{v}</text>
          </g>
        ))}
        {points.map((p, i) => labelIdx.has(i) && <text key={p.label} x={x(i)} y={H - 8} textAnchor={i === 0 ? 'start' : i === points.length - 1 ? 'end' : 'middle'} className="viz-axis">{p.label}</text>)}
        <path d={d} fill="none" stroke="var(--series-1)" strokeWidth="2" strokeLinejoin="round" strokeLinecap="round" />
        {hover >= 0 && <line x1={x(hover)} x2={x(hover)} y1={T} y2={H - B} className="viz-cross" />}
        {hover >= 0 && <circle cx={x(hover)} cy={y(points[hover].value)} r="5" fill="var(--series-1)" stroke="var(--paper)" strokeWidth="2" />}
        {points.map((p, i) => (
          <rect key={i} x={x(i) - (W - L - R) / (points.length - 1 || 1) / 2} y={T} width={(W - L - R) / (points.length - 1 || 1)} height={H - T - B} fill="transparent"
            onMouseMove={(e) => { setHover(i); t.show(e, [p.label, `${p.value} ${unit}`]) }}
            onTouchStart={(e) => { setHover(i); t.show(e, [p.label, `${p.value} ${unit}`]) }}
            onMouseLeave={() => { setHover(-1); t.hide() }} />
        ))}
      </svg>
      <Tip tip={t.tip} />
    </div>
  )
}
