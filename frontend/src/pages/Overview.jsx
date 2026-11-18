import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api } from '../api.js'
import { useAuth } from '../auth.jsx'
import { LEVEL, LEAD_STATUS, timeAgo } from '../ui.jsx'
import { LineChart, StackedBars, OUTCOME, outcomeParts } from './Charts.jsx'
import { LEAD_CHANNELS } from '../ui.jsx'

const BAND = { high: 'คะแนนสูง (50 ขึ้นไป)', mid: 'คะแนนกลาง (25–49)', low: 'คะแนนต่ำ (ต่ำกว่า 25)' }
const LEVELS = { hot: 'Hot', warm: 'Warm', cold: 'Cold', disqualified: 'ตัดสิทธิ์' }
// leads created per day, last 14 days (browser date; list comes from /leads)
function perDay(leads) {
  const days = []
  for (let i = 13; i >= 0; i--) { const t = new Date(); t.setDate(t.getDate() - i); days.push(t) }
  const key = (t) => `${t.getFullYear()}-${String(t.getMonth() + 1).padStart(2, '0')}-${String(t.getDate()).padStart(2, '0')}`
  const n = {}
  leads.forEach((l) => { if (l.created_at) { const k = key(new Date(l.created_at)); n[k] = (n[k] || 0) + 1 } })
  return days.map((t) => ({ label: `${String(t.getDate()).padStart(2, '0')}/${String(t.getMonth() + 1).padStart(2, '0')}`, value: n[key(t)] || 0 }))
}

export default function Overview() {
  const { user } = useAuth()
  const [d, setD] = useState(null)
  const [error, setError] = useState('')
  useEffect(() => {
    Promise.all([api('GET', '/leads'), api('GET', '/contents?status=pending_approval'), api('GET', '/campaigns'), api('GET', '/inbox'), api('GET', '/reports/summary').catch(() => null)])
      .then(([leads, pending, campaigns, inbox, rep]) => setD({ leads, pending, campaigns, inbox, rep }))
      .catch((e) => setError(e.message))
  }, [])
  if (error) return <div className="page"><div className="alert alert-error" role="alert">{error}</div></div>
  if (!d) return <div className="page muted pad">กำลังโหลด…</div>
  const { leads, pending, campaigns, inbox, rep } = d
  const todo = [
    inbox.leads_ready > 0 && { to: '/leads', text: `${inbox.leads_ready} ลีดพร้อมส่งต่อ รอเซลส์รับ${inbox.leads_ready_oldest ? ` (รอนานสุด ${timeAgo(inbox.leads_ready_oldest).replace(' ที่แล้ว', '')})` : ''}` },
    inbox.leads_unassigned > 0 && { to: '/leads', text: `${inbox.leads_unassigned} ลีดที่ถึงเกณฑ์แต่ยังไม่มีผู้รับผิดชอบ` },
    inbox.my_leads > 0 && { to: '/leads', text: `${inbox.my_leads} ลีดที่คุณรับผิดชอบอยู่` },
    inbox.messages_unread > 0 && { to: '/leads', text: `${inbox.messages_unread} ลีดมีข้อความใหม่จากลูกค้า รอเซลส์ตอบ` },
    inbox.contents_to_approve > 0 && { to: '/approvals', text: `${inbox.contents_to_approve} คอนเทนต์รอคุณอนุมัติ` },
    inbox.followups_to_approve > 0 && { to: '/followups', text: `${inbox.followups_to_approve} ร่างข้อความติดตามลีดรออนุมัติ` },
  ].filter(Boolean)
  const by = (k, v) => leads.filter((l) => l[k] === v).length
  const ready = leads.filter((l) => l.status === 'qualified')
  const top = leads.filter((l) => l.status !== 'closed' && l.status !== 'lost').slice(0, 5)
  return (
    <div className="page">
      <header className="page-head"><div><h1>สวัสดี {user.name || user.email}</h1><p className="muted">สรุปงานในวงจร: แคมเปญ → คอนเทนต์ → อนุมัติ → ลีดและคะแนน → ส่งต่อเซลส์</p></div></header>
      <section className="todo" aria-label="ต้องทำตอนนี้">
        <h2>ต้องทำตอนนี้</h2>
        {todo.length === 0 ? <p className="muted">ไม่มีงานค้างสำหรับคุณ</p> : <ul>{todo.map((t) => <li key={t.to}><Link className="link" to={t.to}>{t.text}</Link></li>)}</ul>}
      </section>
      <div className="stats">
        <Link className="stat" to="/campaigns"><b>{campaigns.length}</b><span>แคมเปญทั้งหมด</span></Link>
        <Link className="stat" to="/approvals"><b>{pending.length}</b><span>คอนเทนต์รออนุมัติ</span></Link>
        <Link className="stat" to="/leads"><b>{leads.length}</b><span>ลีดทั้งหมด</span></Link>
        <Link className="stat" to="/leads"><b>{ready.length}</b><span>ลีดพร้อมส่งต่อเซลส์</span></Link>
        <Link className="stat" to="/leads"><b>{by('status', 'handed_off')}</b><span>ส่งต่อเซลส์แล้ว</span></Link>
      </div>
      <div className="grid2">
      <section className="panel">
        <h2>ลีดใหม่ในแต่ละวัน (14 วันล่าสุด)</h2>
        <LineChart points={perDay(leads)} />
      </section>
        <section className="panel">
          <h2>ลีดตามระดับและผลลัพธ์</h2>
          <StackedBars series={OUTCOME} rows={Object.keys(LEVELS).map((k) => ({ label: LEVELS[k], parts: { open: leads.filter((l) => l.level === k && l.status !== 'closed' && l.status !== 'lost').length, lost: leads.filter((l) => l.level === k && l.status === 'lost').length, closed: leads.filter((l) => l.level === k && l.status === 'closed').length } }))} />
        </section>
      </div>
      {rep && (
        <div className="grid2" style={{ marginTop: 16 }}>
          <section className="panel">
            <h2>ผลตามช่องทาง</h2>
            <StackedBars series={OUTCOME} rows={rep.by_channel.map((c) => ({ label: LEAD_CHANNELS[c.channel] || c.channel, parts: outcomeParts(c) }))} />
          </section>
          <section className="panel">
            <h2>ผลตามแคมเปญ</h2>
            <StackedBars series={OUTCOME} rows={rep.by_campaign.map((c) => ({ label: c.campaign, parts: outcomeParts(c) }))} />
          </section>
          <section className="panel">
            <h2>ผลตามกลุ่มคะแนน</h2>
            <StackedBars series={OUTCOME} rows={rep.score_bands.map((b) => ({ label: BAND[b.band], parts: outcomeParts(b) }))} />
          </section>
        </div>
      )}
      <section className="panel" style={{ marginTop: 16 }}>
          <h2>ลีดคะแนนสูงที่ยังเปิดอยู่</h2>
          {top.length === 0 ? <p className="muted">ยังไม่มีลีด</p> : (
            <ul className="bd">
              {top.map((l) => <li key={l.id}><Link className="link" to={`/leads/${l.id}`}>{l.display_name}</Link><span className="muted small">{LEAD_STATUS[l.status]} · <b className="pts">{l.score}</b> · {timeAgo(l.created_at)}</span></li>)}
            </ul>
          )}
        </section>
    </div>
  )
}
