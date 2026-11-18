import { useEffect, useState } from 'react'
import { api } from '../api.js'
import { LEAD_CHANNELS, LEAD_STATUS } from '../ui.jsx'

const BAND = { high: 'คะแนนสูง (50 ขึ้นไป)', mid: 'คะแนนกลาง (25–49)', low: 'คะแนนต่ำ (ต่ำกว่า 25)' }
const pct = (a, b) => (b > 0 ? `${Math.round((a / b) * 100)}%` : '-')

function insights(d) {
  const out = []
  if (d.total < 30) out.push(`ข้อมูลยังน้อย (${d.total} ลีด) ใช้ดูแนวโน้มเท่านั้น ยังสรุปผลไม่ได้`)
  const done = (b) => b.closed + b.lost
  const high = d.score_bands.find((b) => b.band === 'high'), low = d.score_bands.find((b) => b.band === 'low')
  if (high && done(high) >= 3 && high.lost > high.closed) out.push('กลุ่มคะแนนสูงปิดการขายไม่สำเร็จมากกว่าสำเร็จ ลองทบทวนกฎคะแนนหรือเกณฑ์ส่งต่อ (50) ว่าส่งลีดที่ยังไม่พร้อมไปให้เซลส์หรือไม่')
  if (low && low.closed > 0) out.push(`มีลีดคะแนนต่ำที่ปิดการขายได้ ${low.closed} ราย ลองดูว่าเหตุการณ์ใดที่กฎยังไม่ให้คะแนน`)
  const ch = d.by_channel.filter((c) => c.leads >= 3 && c.closed > 0).sort((a, b) => b.closed / b.leads - a.closed / a.leads)[0]
  if (ch) out.push(`ช่องทางที่ปิดการขายได้สัดส่วนสูงสุดตอนนี้คือ ${LEAD_CHANNELS[ch.channel] || ch.channel} (${pct(ch.closed, ch.leads)} ของลีด)`)
  if (out.length === 0 || (out.length === 1 && d.total < 30)) out.push('ยังไม่พบสัญญาณที่ควรปรับ เมื่อมีลีดที่ปิดหรือไม่สำเร็จมากขึ้น ระบบจะแสดงข้อเสนอแนะที่นี่')
  return out
}

function Table({ title, note, head, rows }) {
  return (
    <section className="panel" style={{ marginBottom: 18 }}>
      <h2>{title}</h2>
      {note && <p className="muted small">{note}</p>}
      <div className="table-wrap"><table className="table">
        <thead><tr>{head.map((h, i) => <th key={h} className={i ? 'right' : ''}>{h}</th>)}</tr></thead>
        <tbody>{rows.length === 0 ? <tr><td colSpan={head.length} className="empty-row">ยังไม่มีข้อมูล</td></tr> : rows.map((r, i) => <tr key={i}>{r.map((c, j) => <td key={j} className={j ? 'right nowrap' : ''}>{c}</td>)}</tr>)}</tbody>
      </table></div>
    </section>
  )
}

export default function Reports() {
  const [d, setD] = useState(null)
  const [error, setError] = useState('')
  useEffect(() => { api('GET', '/reports/summary').then(setD).catch((e) => setError(e.message)) }, [])
  if (error) return <div className="page"><div className="alert alert-error" role="alert">{error}</div></div>
  if (!d) return <div className="page muted pad">กำลังโหลด…</div>
  const max = Math.max(1, ...Object.values(d.funnel))
  return (
    <div className="page">
      <header className="page-head"><div><h1>รายงาน</h1><p className="muted">สรุปว่าลีดมาจากช่องทางและแคมเปญไหน ไปถึงเซลส์เท่าไร ปิดการขายได้เท่าไร และกฎคะแนนสอดคล้องกับผลจริงหรือไม่ (นับสถานะปัจจุบันของลีด ไม่รวมลีดที่ลบข้อมูลแล้ว)</p></div></header>
      <section className="todo" aria-label="ข้อสังเกต">
        <h2>ข้อสังเกตจากข้อมูลตอนนี้</h2>
        <ul>{insights(d).map((t, i) => <li key={i}>{t}</li>)}</ul>
      </section>
      <section className="panel" style={{ marginBottom: 18 }}>
        <h2>ขั้นตอนของลีด ({d.total} ลีด)</h2>
        <ul className="bd">
          {Object.keys(LEAD_STATUS).map((k) => (
            <li key={k}><span style={{ minWidth: 110 }}>{LEAD_STATUS[k]}</span>
              <span className="bar" style={{ flex: 1, margin: '0 12px' }} aria-hidden="true"><i style={{ width: `${((d.funnel[k] || 0) / max) * 100}%` }} /></span>
              <b className="pts">{d.funnel[k] || 0}</b></li>
          ))}
        </ul>
      </section>
      <Table title="ตามช่องทาง" head={['ช่องทาง', 'ลีด', 'ถึงเซลส์', 'ปิดการขาย', 'ไม่สำเร็จ', 'คะแนนเฉลี่ย']}
        rows={d.by_channel.map((c) => [LEAD_CHANNELS[c.channel] || c.channel, c.leads, `${c.reached_sales} (${pct(c.reached_sales, c.leads)})`, `${c.closed} (${pct(c.closed, c.leads)})`, c.lost, c.avg_score])} />
      <Table title="ตามแคมเปญ" head={['แคมเปญ', 'ลีด', 'ถึงเซลส์', 'ปิดการขาย', 'ไม่สำเร็จ', 'คะแนนเฉลี่ย']}
        rows={d.by_campaign.map((c) => [c.campaign, c.leads, `${c.reached_sales} (${pct(c.reached_sales, c.leads)})`, `${c.closed} (${pct(c.closed, c.leads)})`, c.lost, c.avg_score])} />
      <Table title="กฎคะแนนตรงกับผลจริงหรือไม่" note="ถ้ากลุ่มคะแนนสูงปิดการขายได้มากกว่ากลุ่มต่ำ แปลว่ากฎให้คะแนนได้ถูกทาง ถ้าไม่ ให้ปรับกฎที่หน้า กฎคะแนน" head={['กลุ่มคะแนน', 'ลีด', 'ปิดการขาย', 'ไม่สำเร็จ', 'อัตราปิดจากที่จบแล้ว']}
        rows={d.score_bands.map((b) => [BAND[b.band], b.leads, b.closed, b.lost, pct(b.closed, b.closed + b.lost)])} />
    </div>
  )
}
