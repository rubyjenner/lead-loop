import { useCallback, useEffect, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { api } from '../api.js'
import { useAuth } from '../auth.jsx'
import { LEAD_CHANNELS, LEAD_STATUS, Toast, baht, canManage, eventLabel } from '../ui.jsx'
import { LevelTag, ScoreCell } from './Leads.jsx'

export default function LeadDetail() {
  const { id } = useParams()
  const nav = useNavigate()
  const { user } = useAuth()
  const [lead, setLead] = useState(null)
  const [error, setError] = useState('')
  const [text, setText] = useState('')
  const [toast, setToast] = useState('')
  const [busy, setBusy] = useState(false)
  const [replies, setReplies] = useState([])
  const [replyText, setReplyText] = useState('')

  const load = useCallback(async () => {
    try { setLead(await api('GET', `/leads/${id}`)); setError('') } catch (e) { setError(e.message) }
  }, [id])
  useEffect(() => { load() }, [load])
  const loadReplies = useCallback(async () => {
    try { setReplies(await api('GET', `/leads/${id}/replies`)) } catch { /* the page still works without it */ }
  }, [id])
  useEffect(() => { loadReplies() }, [loadReplies])

  async function run(fn, ok) {
    setBusy(true); setError('')
    try { const r = await fn(); if (ok) setToast(typeof ok === 'function' ? ok(r) : ok); await load() } catch (e) { setError(e.message) } finally { setBusy(false) }
  }
  const sendMsg = (e) => {
    e.preventDefault()
    run(() => api('POST', `/leads/${id}/message`, { text }), (r) => (r.detected?.length ? `จับได้: ${r.detected.map(eventLabel).join(', ')}` : 'ไม่พบคำที่ตรงกับเหตุการณ์ใดเลย'))
    setText('')
  }
  const sendReply = (e) => {
    e.preventDefault()
    const body = replyText.trim()
    if (!body) return
    run(() => api('POST', `/leads/${id}/reply`, { body }), 'ส่งข้อความถึงลูกค้าแล้ว').then(() => { setReplyText(''); loadReplies() })
  }
  const setStatus = (status) => run(() => api('POST', `/leads/${id}/status`, { status }), 'อัปเดตสถานะแล้ว')
  const consent = (kind, action) => run(() => api('POST', `/leads/${id}/consent`, { kind, action, text_version: 'mock-v1' }), 'บันทึกความยินยอมแล้ว')
  async function anonymize() {
    if (!confirm('ลบข้อมูลระบุตัวตนของลีดนี้ถาวร ย้อนกลับไม่ได้ ยืนยันหรือไม่?')) return
    try { await api('DELETE', `/leads/${id}`); nav('/leads') } catch (e) { setError(e.message) }
  }

  if (error && !lead) return <div className="page"><Link className="crumb" to="/leads">← ลีดทั้งหมด</Link><div className="alert alert-error" role="alert">{error}</div></div>
  if (!lead) return <div className="page muted pad">กำลังโหลด…</div>
  const canSales = user.role === 'sales' || user.role === 'admin'
  const consentOk = lead.consent_general
  const canReply = (lead.channel === 'web' || lead.channel === 'line') && consentOk
  return (
    <div className="page">
      <Link className="crumb" to="/leads">← ลีดทั้งหมด</Link>
      <header className="page-head">
        <div>
          <h1>{lead.display_name}</h1>
          <p className="muted">{LEAD_CHANNELS[lead.channel] || lead.channel} · งบ {lead.budget != null ? baht(lead.budget) : 'ไม่ระบุ'}</p>
        </div>
        <div className="actions"><LevelTag level={lead.level} /><span className={`tag tag-l-${lead.status}`}>{LEAD_STATUS[lead.status] || lead.status}</span></div>
      </header>
      {error && <div className="alert alert-error" role="alert">{error}</div>}
      <div className="grid2">
        <section className="panel">
          <h2>คะแนน {lead.score} / 100</h2>
          <ScoreCell score={lead.score} />
          <p className="muted small">ได้คะแนนมาจากกฎเหล่านี้</p>
          {lead.score_breakdown.length === 0 ? <p className="muted">ยังไม่มีเหตุการณ์ที่ให้คะแนน</p> : (
            <ul className="bd">
              {lead.score_breakdown.map((b, i) => (
                <li key={i}><span>{b.rule}</span><span className={`pts ${b.points < 0 ? 'pts-neg' : 'pts-pos'}`}>{b.points > 0 ? '+' : ''}{b.points}</span></li>
              ))}
            </ul>
          )}
          {canSales && (
            <div className="actions" style={{ marginTop: 14 }}>
              {lead.status === 'qualified' && <button className="btn btn-primary" disabled={busy} onClick={() => setStatus('handed_off')}>รับลีดนี้ (ส่งต่อเซลส์)</button>}
              {(lead.status === 'qualified' || lead.status === 'handed_off') && <>
                <button className="btn btn-quiet" disabled={busy} onClick={() => setStatus('closed')}>ปิดการขายได้</button>
                <button className="btn btn-quiet" disabled={busy} onClick={() => setStatus('lost')}>ไม่สำเร็จ</button>
              </>}
              {lead.status !== 'qualified' && lead.status !== 'handed_off' && <span className="muted small">ส่งต่อได้เมื่อสถานะเป็น "พร้อมส่งต่อ"</span>}
            </div>
          )}
        </section>
        <section className="panel">
          <h2>ความยินยอม (PDPA)</h2>
          <div className="consent">
            <span className={`tag ${consentOk ? 'tag-l-closed' : 'tag-l-lost'}`}>ข้อมูลทั่วไป: {consentOk ? 'ยินยอม' : 'ถอนแล้ว'}</span>
            <span className={`tag ${lead.consent_marketing ? 'tag-l-closed' : 'tag-l-new'}`}>การตลาด: {lead.consent_marketing ? 'ยินยอม' : 'ไม่ยินยอม'}</span>
          </div>
          {canManage(user) && (
            <div className="actions" style={{ marginTop: 12 }}>
              {lead.consent_marketing
                ? <button className="btn btn-quiet" disabled={busy} onClick={() => consent('marketing', 'withdrawn')}>ถอนความยินยอมการตลาด</button>
                : consentOk && <button className="btn btn-quiet" disabled={busy} onClick={() => consent('marketing', 'granted')}>บันทึกว่ายินยอมรับข่าวสาร</button>}
              {consentOk && <button className="btn btn-danger" disabled={busy} onClick={() => consent('general', 'withdrawn')}>ถอนความยินยอมทั้งหมด</button>}
            </div>
          )}
          <p className="muted small">ถอนความยินยอมแล้ว ระบบจะไม่ประมวลผลข้อความหรือส่งข้อความหาลีดนี้อีก</p>
          {canManage(user) && (
            <>
              <h2 style={{ marginTop: 18 }}>ส่งข้อความจำลอง</h2>
              <form className="form" onSubmit={sendMsg}>
                <label className="field"><span>พิมพ์แทนลูกค้า (ระบบเก็บเฉพาะเหตุการณ์ที่จับได้ ไม่เก็บข้อความ)</span>
                  <input value={text} onChange={(e) => setText(e.target.value)} placeholder="เช่น ขอราคาและรายละเอียดหน่อยค่ะ" disabled={!consentOk} /></label>
                <button className="btn btn-primary" disabled={busy || !text.trim() || !consentOk}>ส่งข้อความ</button>
              </form>
            </>
          )}
        </section>
      </div>
      {(lead.channel === 'web' || lead.channel === 'line') && (
        <section className="panel" style={{ marginTop: 16 }}>
          <h2>ตอบลูกค้า</h2>
          <p className="muted small">{lead.channel === 'line'
            ? 'ข้อความจะถูกส่งเข้า LINE ของลูกค้าคนนี้ทันที (นับเป็นโควตาข้อความของบัญชี LINE) หลังตอบแล้วบอตจะหยุดตอบลูกค้าคนนี้ ห้ามใส่ข้อมูลส่วนตัวของผู้อื่น'
            : 'ข้อความจะไปแสดงในหน้าแชตของลูกค้าคนนี้ (ลูกค้ากำลังเปิดหน้าแชตอยู่ถึงจะเห็น) ห้ามใส่ข้อมูลส่วนตัวของผู้อื่น'}</p>
          {replies.length > 0 && (
            <ul className="bd">
              {replies.map((r) => <li key={r.id}><span>{r.body}</span><span className="muted small">{r.sent_by} · {new Date(r.created_at).toLocaleString('th-TH')}</span></li>)}
            </ul>
          )}
          {canReply ? (
            <form className="form" onSubmit={sendReply}>
              <label className="field"><span>ข้อความถึงลูกค้า (ไม่เกิน 500 ตัวอักษร)</span>
                <textarea rows={3} maxLength={500} value={replyText} onChange={(e) => setReplyText(e.target.value)} placeholder="เช่น สวัสดีค่ะ ทางร้านยินดีให้ข้อมูลเพิ่มเติม สะดวกให้โทรกลับช่วงไหนคะ" /></label>
              <button className="btn btn-primary" disabled={busy || !replyText.trim()}>ส่งถึงลูกค้า</button>
            </form>
          ) : <p className="muted small">ลูกค้าถอนความยินยอมแล้ว จึงส่งข้อความไม่ได้</p>}
        </section>
      )}
      {user.role === 'admin' && <p style={{ marginTop: 18 }}><button className="btn btn-danger" onClick={anonymize}>ลบข้อมูลระบุตัวตนของลีดนี้</button></p>}
      <Toast msg={toast} onDone={() => setToast('')} />
    </div>
  )
}
