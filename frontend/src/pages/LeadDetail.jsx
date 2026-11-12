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
  const [thread, setThread] = useState([])
  const [replyText, setReplyText] = useState('')
  const [owner, setOwner] = useState(null)
  const [team, setTeam] = useState([])

  const load = useCallback(async () => {
    try { setLead(await api('GET', `/leads/${id}`)); setError('') } catch (e) { setError(e.message) }
  }, [id])
  useEffect(() => { load() }, [load])
  const loadOwner = useCallback(async () => {
    try { setOwner((await api('GET', '/lead-assignments')).find((x) => String(x.lead_id) === String(id)) || null) } catch { /* optional */ }
  }, [id])
  useEffect(() => { loadOwner() }, [loadOwner])
  useEffect(() => { if (user.role === 'admin') api('GET', '/sales-team').then(setTeam).catch(() => {}) }, [user.role])
  async function assign(email) {
    setBusy(true)
    try { await api('PUT', `/leads/${id}/assignment`, { assignee: email }); await loadOwner(); setToast(email ? 'บันทึกผู้รับผิดชอบแล้ว' : 'ปล่อยลีดแล้ว') } catch (e) { setError(e.message) } finally { setBusy(false) }
  }
  const loadThread = useCallback(async () => {
    try { setThread(await api('GET', `/leads/${id}/thread`)) } catch { /* the page still works without it */ }
  }, [id])
  useEffect(() => {
    loadThread()
    const t = setInterval(loadThread, 5000) // new messages from the customer show up without reloading
    return () => clearInterval(t)
  }, [loadThread])

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
    run(() => api('POST', `/leads/${id}/reply`, { body }), 'ส่งข้อความถึงลูกค้าแล้ว').then(() => { setReplyText(''); loadThread() })
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
          <p className="muted">
            {lead.campaign_name ? <>แคมเปญ <Link className="link" to={`/campaigns/${lead.campaign_id}`}>#{lead.campaign_id} {lead.campaign_name}</Link>{lead.product_name ? ` · สินค้า ${lead.product_name}` : ''} · </> : 'ไม่ผูกแคมเปญ · '}
            ช่องทาง {LEAD_CHANNELS[lead.channel] || lead.channel} · งบ {lead.budget != null ? baht(lead.budget) : 'ไม่ระบุ'}</p>
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
            <div className="actions" style={{ marginTop: 14, alignItems: 'center' }}>
              <span className="muted small">ผู้รับผิดชอบ: <b>{owner ? owner.name : 'ยังไม่มี'}</b></span>
              {!owner && <button className="btn btn-quiet" disabled={busy} onClick={() => assign(user.email)}>รับเป็นผู้รับผิดชอบ</button>}
              {owner && owner.email === user.email && <button className="btn btn-quiet" disabled={busy} onClick={() => assign('')}>ปล่อยลีด</button>}
              {user.role === 'admin' && team.length > 0 && (
                <select className="search" value={owner?.email || ''} disabled={busy} aria-label="มอบหมายให้" onChange={(e) => assign(e.target.value)}>
                  <option value="">มอบหมายให้…</option>
                  {team.map((t) => <option key={t.email} value={t.email}>{t.name}</option>)}
                </select>
              )}
            </div>
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
            ? 'ข้อความจะถูกส่งเข้า LINE ของลูกค้าคนนี้ทันที (นับเป็นโควตาข้อความของบัญชี LINE) ห้ามใส่ข้อมูลส่วนตัวของผู้อื่น'
            : 'ข้อความจะไปแสดงในหน้าแชตของลูกค้าคนนี้ (ลูกค้ากำลังเปิดหน้าแชตอยู่ถึงจะเห็น) ห้ามใส่ข้อมูลส่วนตัวของผู้อื่น'}</p>
          <p className="muted small">เมื่อลีดเป็น "พร้อมส่งต่อ" หรือ "ส่งต่อเซลส์แล้ว" บอตจะหยุดตอบ และข้อความที่ลูกค้าพิมพ์ต่อจากนี้จะขึ้นที่นี่ จนกว่าจะกด "ปิดการขายได้" หรือ "ไม่สำเร็จ"</p>
          {thread.length === 0
            ? <p className="muted small">ยังไม่มีข้อความในบทสนทนานี้</p>
            : (
              <div className="thread" aria-live="polite">
                {thread.map((m) => (
                  <div key={`${m.dir}-${m.id}`} className={`thread-msg thread-${m.dir}`}>
                    <span>{m.body}</span>
                    <span className="muted small">{m.dir === 'in' ? 'ลูกค้า' : m.sent_by || 'เจ้าหน้าที่'} · {new Date(m.created_at).toLocaleString('th-TH')}</span>
                  </div>
                ))}
              </div>
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
