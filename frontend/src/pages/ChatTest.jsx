import { useEffect, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { api } from '../api.js'
import { LEAD_STATUS, eventLabel } from '../ui.jsx'
import { LevelTag, ScoreCell } from './Leads.jsx'

// "Customer simulator": the team plays a customer in the mock chat channel, so the whole flow
// (consent -> messages -> score -> hand-off) can be tried from the browser, the way a real customer would.
const QUICK = ['สนใจค่ะ ขอรายละเอียดหน่อย', 'ราคาเท่าไหร่คะ', 'ผ่อนได้ไหม', 'ขอคุยกับเจ้าหน้าที่', 'ช่วยโทรกลับด้วยนะคะ', 'หยุด']
const newExt = () => `web-test-${Math.random().toString(36).slice(2, 8)}`

export default function ChatTest() {
  const [campaigns, setCampaigns] = useState([])
  const [campaignId, setCampaignId] = useState('')
  const [name, setName] = useState('ลูกค้าทดสอบ')
  const [ext, setExt] = useState(newExt)
  const [log, setLog] = useState([{ from: 'sys', text: 'นี่คือหน้าจำลองลูกค้า พิมพ์ข้อความหรือกดปุ่มด้านล่างเหมือนลูกค้าคุยกับร้าน ใช้ข้อมูลทดสอบเท่านั้น' }])
  const [text, setText] = useState('')
  const [pending, setPending] = useState(null) // message waiting for the consent answer
  const [marketing, setMarketing] = useState(false)
  const [lead, setLead] = useState(null)
  const [last, setLast] = useState([])
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const end = useRef(null)

  useEffect(() => { api('GET', '/campaigns').then(setCampaigns).catch(() => {}) }, [])
  useEffect(() => { end.current?.scrollIntoView({ block: 'end' }) }, [log, pending])

  const push = (m) => setLog((l) => [...l, m])
  async function refreshLead(id) { try { setLead(await api('GET', `/leads/${id}`)) } catch { /* lead may be gone */ } }

  async function send(t) {
    const msg = (t ?? text).trim()
    if (!msg || busy) return
    setText(''); setError(''); setBusy(true)
    push({ from: 'cust', text: msg })
    try {
      const r = await api('POST', '/channels/mock/messages', { external_id: ext, text: msg })
      push({ from: 'bot', text: r.reply })
      if (r.needs_consent) { setPending(msg); setLast([]) } else {
        setPending(null); setLast(r.detected || [])
        if (r.lead_id) await refreshLead(r.lead_id)
      }
    } catch (e) { setError(e.message) } finally { setBusy(false) }
  }

  async function accept() {
    setBusy(true); setError('')
    try {
      const body = { external_id: ext, display_name: name.trim() || 'ลูกค้าทดสอบ', consent_marketing: marketing }
      if (campaignId) body.campaign_id = Number(campaignId)
      const r = await api('POST', '/channels/mock/consent', body)
      push({ from: 'cust', text: marketing ? 'ยอมรับ (รวมรับข่าวสารการตลาด)' : 'ยอมรับ' })
      push({ from: 'bot', text: r.reply })
      const again = pending; setPending(null)
      await refreshLead(r.lead_id)
      if (again) { setBusy(false); await send(again); return }
    } catch (e) { setError(e.message) } finally { setBusy(false) }
  }

  function decline() {
    push({ from: 'cust', text: 'ไม่ยอมรับ' })
    push({ from: 'sys', text: 'ลูกค้าไม่ยอมรับ ระบบจึงไม่เก็บข้อมูลใดๆ ของลูกค้ารายนี้ (ไม่มีลีดถูกสร้าง)' })
    setPending(null)
  }

  function reset() {
    setExt(newExt()); setLead(null); setLast([]); setPending(null); setError('')
    setLog([{ from: 'sys', text: 'เริ่มลูกค้าคนใหม่แล้ว' }])
  }

  return (
    <div className="page">
      <header className="page-head">
        <div>
          <h1>ทดลองแชตลูกค้า</h1>
          <p className="muted">ลองเป็นลูกค้า: ขอความยินยอม → ทักสนใจ/ถามราคา → ดูคะแนนและการส่งต่อเซลส์เปลี่ยนทันทีทางขวา (ช่องทางแชตจำลอง ไม่ใช่ LINE จริง)</p>
        </div>
        <button className="btn btn-quiet" onClick={reset}>เริ่มลูกค้าคนใหม่</button>
      </header>
      <div className="alert alert-info">ข้อมูลทดสอบเท่านั้น ห้ามใส่ชื่อหรือข้อมูลลูกค้าจริง ระบบไม่เก็บตัวข้อความ เก็บเฉพาะเหตุการณ์ที่จับได้</div>
      <div className="chat-grid">
        <section className="chat">
          <div className="chat-setup">
            <label className="field"><span>ชื่อลูกค้า (สมมติ)</span><input value={name} onChange={(e) => setName(e.target.value)} disabled={!!lead} /></label>
            <label className="field"><span>แคมเปญที่ลูกค้าเข้ามาจาก</span>
              <select value={campaignId} onChange={(e) => setCampaignId(e.target.value)} disabled={!!lead}>
                <option value="">ไม่ระบุ (ตอบได้เฉพาะข้อความทั่วไป)</option>
                {campaigns.map((c) => <option key={c.id} value={c.id}>{c.name}</option>)}
              </select></label>
          </div>
          <div className="chat-log" aria-live="polite">
            {log.map((m, i) => <div key={i} className={`bubble bubble-${m.from}`}>{m.text}</div>)}
            {pending && (
              <div className="consent-card">
                <b>ขอความยินยอมก่อนคุยต่อ</b>
                <label className="checks"><input type="checkbox" checked={marketing} onChange={(e) => setMarketing(e.target.checked)} /> ยินยอมรับข่าวสารและโปรโมชันจากร้าน (ไม่บังคับ)</label>
                <div className="actions">
                  <button className="btn btn-primary" onClick={accept} disabled={busy}>ยอมรับและเริ่มคุย</button>
                  <button className="btn btn-quiet" onClick={decline} disabled={busy}>ไม่ยอมรับ</button>
                </div>
              </div>
            )}
            <div ref={end} />
          </div>
          {error && <div className="alert alert-error" role="alert">{error}</div>}
          <div className="chips">{QUICK.map((q) => <button key={q} className="chip" onClick={() => send(q)} disabled={busy || !!pending}>{q}</button>)}</div>
          <form className="chat-input" onSubmit={(e) => { e.preventDefault(); send() }}>
            <input value={text} onChange={(e) => setText(e.target.value)} placeholder="พิมพ์ข้อความในฐานะลูกค้า" disabled={!!pending} />
            <button className="btn btn-primary" disabled={busy || !!pending || !text.trim()}>ส่ง</button>
          </form>
        </section>
        <aside className="panel">
          <h2>มุมมองทีมงาน (สดๆ)</h2>
          {!lead ? <p className="muted">ยังไม่มีลีด ลูกค้าต้องยอมรับความยินยอมก่อน ระบบถึงจะสร้างลีดและเก็บข้อมูล</p> : (
            <>
              <p><Link className="link" to={`/leads/${lead.id}`}>{lead.display_name}</Link> · {LEAD_STATUS[lead.status] || lead.status}</p>
              <p className="actions"><LevelTag level={lead.level} />{lead.status === 'qualified' && <span className="tag tag-l-qualified">พร้อมส่งต่อเซลส์</span>}</p>
              <ScoreCell score={lead.score} />
              <p className="muted small" style={{ marginTop: 10 }}>เหตุการณ์ที่จับได้จากข้อความล่าสุด</p>
              <p>{last.length ? last.map((e) => <span key={e} className="tag tag-warn" style={{ marginRight: 6 }}>{eventLabel(e)}</span>) : <span className="muted">ไม่พบ</span>}</p>
              <p className="muted small">คะแนนมาจาก</p>
              <ul className="bd">
                {lead.score_breakdown.length === 0 && <li className="muted">ยังไม่มี</li>}
                {lead.score_breakdown.map((b, i) => <li key={i}><span>{b.rule}</span><span className={`pts ${b.points < 0 ? 'pts-neg' : 'pts-pos'}`}>{b.points > 0 ? '+' : ''}{b.points}</span></li>)}
              </ul>
              <p className="muted small">การตลาด: {lead.consent_marketing ? 'ยินยอม' : 'ไม่ยินยอม'}</p>
            </>
          )}
        </aside>
      </div>
    </div>
  )
}
