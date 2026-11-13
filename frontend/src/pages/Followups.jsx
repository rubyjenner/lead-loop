import { useCallback, useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api } from '../api.js'
import { useAuth } from '../auth.jsx'
import { LEAD_CHANNELS, Modal, Toast, canManage, fmtDateTime, personLabel, timeAgo } from '../ui.jsx'
import { LevelTag } from './Leads.jsx'

const TABS = [['pending_approval', 'รออนุมัติ'], ['sent', 'ส่งแล้ว'], ['approved', 'อนุมัติแล้ว (รอส่ง)'], ['failed', 'ส่งไม่สำเร็จ'], ['rejected', 'ถูกปฏิเสธ']]

export default function Followups() {
  const { user } = useAuth()
  const [tab, setTab] = useState('pending_approval')
  const [all, setAll] = useState(null)
  const [last, setLast] = useState(undefined)
  const [error, setError] = useState('')
  const [rowError, setRowError] = useState({})
  const [busy, setBusy] = useState(0)
  const [running, setRunning] = useState(false)
  const [toast, setToast] = useState('')
  const [quota, setQuota] = useState(null) // LINE monthly message quota
  const [draftFor, setDraftFor] = useState(null)
  const manage = canManage(user)

  const load = useCallback(async () => {
    try {
      setAll(await api('GET', '/followups'))
      if (user.role === 'admin') setLast((await api('GET', '/daily-jobs/last')).run)
      setError('')
    } catch (e) { setError(e.message) }
    api('GET', '/line/quota').then(setQuota).catch(() => setQuota(null))
  }, [user.role])
  useEffect(() => { load() }, [load])

  async function act(f, action, ok) {
    setBusy(f.id); setRowError((r) => ({ ...r, [f.id]: '' }))
    try { const r = await api('POST', `/followups/${f.id}/${action}`); setToast(typeof ok === 'function' ? ok(r) : ok); await load() } catch (e) {
      setRowError((r) => ({ ...r, [f.id]: e.message })); if (e.status === 409) load()
    } finally { setBusy(0) }
  }
  async function runNow() {
    setRunning(true); setError('')
    try {
      const s = await api('POST', '/daily-jobs/run')
      setToast(`ตรวจ ${s.checked} ลีด · เงียบ 3 วัน ${s.no_reply_3d} · เงียบ 7 วัน ${s.inactive_7d} · ร่างติดตามใหม่ ${s.followups_created}`)
      await load()
    } catch (e) { setError(e.message) } finally { setRunning(false) }
  }

  const count = (s) => (all || []).filter((f) => f.status === s).length
  const shown = (all || []).filter((f) => f.status === tab)
  return (
    <div className="page">
      <header className="page-head">
        <div>
          <h1>ติดตามลีด</h1>
          <p className="muted">ระบบตรวจลีดที่เงียบไปทุกวันเวลา 09:00 แล้วร่างข้อความติดตามให้ ต้องมีคนกดอนุมัติก่อนส่งทุกครั้ง และส่งได้เฉพาะลีดที่ยินยอมรับข่าวสารการตลาด</p>
        </div>
        <div className="actions">{manage && <button className="btn btn-primary" onClick={() => setDraftFor({})}>ร่างข้อความใหม่</button>}{user.role === 'admin' && <button className="btn btn-quiet" onClick={runNow} disabled={running}>{running ? 'กำลังตรวจ…' : 'รันงานรายวันตอนนี้'}</button>}</div>
      </header>
      {error && <div className="alert alert-error" role="alert">{error}</div>}
      {user.role === 'admin' && last !== undefined && (
        <div className="alert alert-info">
          {last ? <>รอบล่าสุด {last.day} ({last.triggered_by === 'schedule' ? 'อัตโนมัติ' : 'สั่งมือ'}) · ตรวจ {last.summary.checked} ลีด · เงียบ 3 วัน {last.summary.no_reply_3d} · เงียบ 7 วัน {last.summary.inactive_7d} · ร่างติดตามใหม่ {last.summary.followups_created}</> : 'ยังไม่เคยรันงานรายวัน'}
        </div>
      )}
      {quota?.enabled && quota.known && !quota.unlimited && (
        <div className={`alert ${quota.remaining <= quota.reserve ? 'alert-warn' : 'alert-info'}`} role="status">
          โควตาข้อความ LINE เดือนนี้: ใช้ไป {quota.used} จาก {quota.limit} (เหลือ {quota.remaining}) ระบบเก็บ {quota.reserve} ข้อความไว้ให้เซลส์ตอบมือ
          {quota.remaining <= quota.reserve && ' ตอนนี้โควตาเหลือน้อย ร่างที่อนุมัติแล้วจะรอจนกว่าจะขึ้นเดือนใหม่หรือเพิ่มโควตา'}
        </div>
      )}
      <div className="alert alert-info">แชตจำลองส่งทันทีเมื่ออนุมัติ ลีดช่องทาง LINE ระบบส่งให้เองภายในราว 30 วินาที (ตรวจความยินยอมอีกครั้งก่อนส่ง และดูโควตาข้อความของ LINE) ช่องทางอื่นยังรอการเชื่อมต่อในสถานะ "อนุมัติแล้ว (รอส่ง)" ถ้า LINE ส่งไม่สำเร็จ 3 ครั้งจะขึ้นเป็น "ส่งไม่สำเร็จ"</div>
      <div className="toolbar">
        <div className="seg" role="group" aria-label="สถานะ">
          {TABS.map(([k, l]) => <button key={k} className={tab === k ? 'seg-on' : ''} aria-pressed={tab === k} onClick={() => setTab(k)}>{l}<span className="seg-n">{all ? count(k) : ''}</span></button>)}
        </div>
      </div>
      {!all && !error && <div className="muted pad">กำลังโหลด…</div>}
      {all && shown.length === 0 && <div className="empty"><p className="empty-title">ไม่มีรายการ</p><p className="muted">เมื่อมีลีดที่เงียบถึงกำหนด ระบบจะร่างข้อความมาให้ที่นี่</p></div>}
      <div className="cards">
        {shown.map((f) => (
          <article key={f.id} className="card">
            <div className="card-top">
              <span><Link className="link" to={`/leads/${f.lead_id}`}>{f.display_name}</Link> <LevelTag level={f.level} /></span>
              <span className="muted small">ครั้งที่ {f.seq} · {LEAD_CHANNELS[f.channel] || f.channel} · {timeAgo(f.created_at)}</span>
            </div>
            <p className="card-body">{f.body}</p>
            <p className="muted small">ร่างโดย: {f.source === 'daily_job' || f.created_by === 'system:daily_job' ? 'ระบบ (งานรายวัน ใช้ข้อความแม่แบบ ไม่ใช่ AI)' : personLabel(f.created_by_name, f.created_by_title, f.created_by)} · ต้องมีคนอนุมัติก่อนส่งทุกครั้ง</p>
            {f.decided_by && <p className="muted small">{f.status === 'rejected' ? 'ปฏิเสธโดย' : 'อนุมัติโดย'} {f.decided_by.startsWith('system:') ? 'ระบบ (ลูกค้าถอนความยินยอม)' : personLabel(f.decided_by_name, f.decided_by_title, f.decided_by)} · {fmtDateTime(f.decided_at)}</p>}
            {rowError[f.id] && <div className="alert alert-error" role="alert">{rowError[f.id]}</div>}
            {f.status === 'pending_approval' && manage && (
              <div className="actions">
                <button className="btn btn-quiet" onClick={() => setDraftFor({ id: f.lead_id, name: f.display_name })}>ร่างข้อความติดตาม</button>
                <button className="btn btn-primary" disabled={busy === f.id} onClick={() => act(f, 'approve', (r) => (r.status === 'sent' ? 'ส่งข้อความแล้ว (แชตจำลอง)' : 'อนุมัติแล้ว'))}>อนุมัติและส่ง</button>
                <button className="btn btn-quiet" disabled={busy === f.id} onClick={() => act(f, 'reject', 'ปฏิเสธแล้ว')}>ปฏิเสธ</button>
              </div>
            )}
          </article>
        ))}
      </div>
      {draftFor && <DraftFollowup lead={draftFor} onClose={() => setDraftFor(null)} onCreated={async () => { setDraftFor(null); setToast('สร้างข้อความรออนุมัติแล้ว'); await load() }} />}
      <Toast msg={toast} onDone={() => setToast('')} />
    </div>
  )
}

function DraftFollowup({ lead, onClose, onCreated }) {
  const [body, setBody] = useState('')
  const [leads, setLeads] = useState([])
  const [leadID, setLeadID] = useState(lead?.id ? String(lead.id) : '')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  useEffect(() => { if (!lead?.id) api('GET', '/leads').then(setLeads).catch((e) => setError(e.message)) }, [lead?.id])
  const selected = lead?.id ? lead : leads.find((x) => String(x.id) === leadID)
  async function submit(e) {
    e.preventDefault(); setError(''); setBusy(true)
    try { await api('POST', `/leads/${selected.id}/followups`, { body }); onCreated() }
    catch (e) { setError(e.message) } finally { setBusy(false) }
  }
  return <Modal title={lead?.id ? `ร่างข้อความติดตาม · ${lead.name}` : 'ร่างข้อความติดตามใหม่'} onClose={onClose}>
    <form className="form" onSubmit={submit}>
      <p className="muted small">ข้อความจะถูกบันทึกเป็นรายการรออนุมัติ ยังไม่ส่งถึงลูกค้าจนกว่าจะมีผู้อนุมัติ และต้องมีความยินยอมด้านการตลาด</p>
      {!lead?.id && <label className="field"><span>เลือกลีด</span><select value={leadID} onChange={(e) => setLeadID(e.target.value)} required><option value="">เลือกลูกค้า</option>{leads.filter((x) => x.consent_marketing).map((x) => <option key={x.id} value={x.id}>{x.display_name} · {x.channel} · {x.level}</option>)}</select></label>}
      {error && <div className="alert alert-error">{error}</div>}
      <label className="field"><span>ข้อความติดตาม</span><textarea rows="5" maxLength={2000} value={body} onChange={(e) => setBody(e.target.value)} placeholder="พิมพ์ข้อความที่ต้องการส่งให้ลูกค้า" autoFocus /></label>
      <div className="modal-foot"><button type="button" className="btn btn-quiet" onClick={onClose}>ยกเลิก</button><button className="btn btn-primary" disabled={busy || !selected?.id || !body.trim()}>{busy ? 'กำลังบันทึก…' : 'บันทึกร่างเพื่อขออนุมัติ'}</button></div>
    </form>
  </Modal>
}
