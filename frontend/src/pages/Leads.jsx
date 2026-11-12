import { useCallback, useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api } from '../api.js'
import { useAuth } from '../auth.jsx'
import { LEAD_CHANNELS, LEAD_STATUS, LEVEL, Modal, Toast, baht, canManage, timeAgo } from '../ui.jsx'

export function LevelTag({ level }) { return <span className={`lvl lvl-${level}`}>{LEVEL[level] || level}</span> }
export function ScoreCell({ score }) {
  return <div className="scorecell"><span className="pts">{score}</span><span className="bar" aria-hidden="true"><i style={{ width: `${Math.min(100, Math.max(0, score))}%` }} /></span></div>
}

export default function Leads() {
  const { user } = useAuth()
  const [list, setList] = useState(null)
  const [error, setError] = useState('')
  const [level, setLevel] = useState('')
  const [status, setStatus] = useState('')
  const [q, setQ] = useState('')
  const [modal, setModal] = useState(false)
  const [toast, setToast] = useState('')
  const [owners, setOwners] = useState({})
  const [mineOnly, setMineOnly] = useState(false)

  const load = useCallback(async () => {
    try {
      setList(await api('GET', `/leads?level=${level}&status=${status}`)); setError('')
      api('GET', '/lead-assignments').then((a) => setOwners(Object.fromEntries(a.map((x) => [x.lead_id, x])))).catch(() => {})
    } catch (e) { setError(e.message) }
  }, [level, status])
  useEffect(() => { load() }, [load])

  const needle = q.trim().toLowerCase()
  const shown = (list || []).filter((l) => (!needle || [l.display_name, LEAD_CHANNELS[l.channel], l.channel, LEAD_STATUS[l.status], owners[l.id]?.name].some((v) => (v || '').toLowerCase().includes(needle))) && (!mineOnly || owners[l.id]?.email === user.email))
  return (
    <div className="page">
      <header className="page-head">
        <div>
          <h1>ลีด</h1>
          <p className="muted">เรียงตามคะแนนจากมากไปน้อย คะแนนมาจากกฎในหน้า "กฎคะแนน" ลีดที่ถึงเกณฑ์จะขึ้นสถานะ "พร้อมส่งต่อ" ให้เซลส์รับ</p>
        </div>
        {canManage(user) && <button className="btn btn-primary" onClick={() => setModal(true)}>เพิ่มลีดทดสอบ</button>}
      </header>
      <div className="alert alert-info">ตอนนี้ใช้ข้อมูลทดสอบเท่านั้น ห้ามใส่ข้อมูลลูกค้าจริงจนกว่าจะได้รับอนุมัติเรื่อง PDPA</div>
      <div className="toolbar">
        <div className="seg" role="group" aria-label="ระดับ">
          {[['', 'ทั้งหมด'], ['hot', 'Hot'], ['warm', 'Warm'], ['cold', 'Cold'], ['disqualified', 'ตัดสิทธิ์']].map(([v, l]) => (
            <button key={v} className={level === v ? 'seg-on' : ''} onClick={() => setLevel(v)}>{l}</button>
          ))}
        </div>
        <select className="search" value={status} onChange={(e) => setStatus(e.target.value)} aria-label="สถานะ">
          <option value="">ทุกสถานะ</option>
          {Object.entries(LEAD_STATUS).map(([k, v]) => <option key={k} value={k}>{v}</option>)}
        </select>
        <input className="search" placeholder="ค้นหาชื่อ ช่องทาง สถานะ หรือผู้รับผิดชอบ" value={q} onChange={(e) => setQ(e.target.value)} />
        {(user.role === 'sales' || user.role === 'admin') && <label className="checks"><input type="checkbox" checked={mineOnly} onChange={(e) => setMineOnly(e.target.checked)} /> เฉพาะลีดของฉัน</label>}
      </div>
      {error && <div className="alert alert-error" role="alert">{error}</div>}
      {!list && !error && <div className="muted pad">กำลังโหลด…</div>}
      {list && shown.length === 0 && <div className="empty"><p className="empty-title">ไม่พบลีด</p><p className="muted">{canManage(user) ? 'กด "เพิ่มลีดทดสอบ" เพื่อสร้างลีดตัวอย่าง แล้วลองส่งข้อความจำลองดูคะแนน' : 'เมื่อมีลีดเข้ามาจะแสดงที่นี่'}</p></div>}
      {shown.length > 0 && (
        <div className="table-wrap">
          <table className="table">
            <thead><tr><th>ชื่อ</th><th>คะแนน</th><th>ระดับ</th><th>สถานะ</th><th>ช่องทาง</th><th>ผู้รับผิดชอบ</th><th className="right">งบ</th><th>เข้ามา</th></tr></thead>
            <tbody>
              {shown.map((l) => (
                <tr key={l.id}>
                  <td><Link className="link" to={`/leads/${l.id}`}>{l.display_name}</Link>{l.unread_messages > 0 && <span className="tag tag-l-qualified" style={{ marginLeft: 8 }}>ข้อความใหม่ {l.unread_messages}</span>}</td>
                  <td><ScoreCell score={l.score} /></td>
                  <td><LevelTag level={l.level} /></td>
                  <td><span className={`tag tag-l-${l.status}`}>{LEAD_STATUS[l.status] || l.status}</span></td>
                  <td>{LEAD_CHANNELS[l.channel] || l.channel}</td>
                  <td className="nowrap">{owners[l.id] ? owners[l.id].name : <span className="muted">-</span>}</td>
                  <td className="right nowrap">{l.budget != null ? baht(l.budget) : <span className="muted">ไม่ระบุ</span>}</td>
                  <td className="muted nowrap">{timeAgo(l.created_at)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {modal && <NewLead onClose={() => setModal(false)} onCreated={() => { setModal(false); setToast('เพิ่มลีดแล้ว'); load() }} />}
      <Toast msg={toast} onDone={() => setToast('')} />
    </div>
  )
}

function NewLead({ onClose, onCreated }) {
  const [f, setF] = useState({ display_name: '', budget: '', consent_general: false, consent_marketing: false })
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  async function submit(e) {
    e.preventDefault(); setError(''); setBusy(true)
    try {
      const body = { display_name: f.display_name.trim(), channel: 'mock', channel_user_id: `mock-${Date.now()}`, consent_general: f.consent_general, consent_marketing: f.consent_marketing, consent_version: 'mock-v1' }
      if (f.budget !== '') body.budget = Number(f.budget)
      await api('POST', '/leads', body)
      onCreated()
    } catch (err) { setError(err.message) } finally { setBusy(false) }
  }
  return (
    <Modal title="เพิ่มลีดทดสอบ" onClose={onClose}>
      <form className="form" onSubmit={submit} noValidate>
        {error && <div className="alert alert-error" role="alert">{error}</div>}
        <label className="field"><span>ชื่อที่แสดง (ใช้ชื่อสมมติ)</span><input value={f.display_name} onChange={(e) => setF({ ...f, display_name: e.target.value })} placeholder="เช่น ลูกค้าทดสอบ 001" autoFocus /></label>
        <label className="field"><span>งบ (บาท) ถ้ายังไม่ทราบให้เว้นว่าง</span><input type="number" min="0" value={f.budget} onChange={(e) => setF({ ...f, budget: e.target.value })} /></label>
        <label className="checks"><input type="checkbox" checked={f.consent_general} onChange={(e) => setF({ ...f, consent_general: e.target.checked })} /> ลูกค้ายินยอมให้เก็บและประมวลผลข้อมูล (จำเป็น)</label>
        <label className="checks"><input type="checkbox" checked={f.consent_marketing} onChange={(e) => setF({ ...f, consent_marketing: e.target.checked })} /> ยินยอมรับข่าวสารการตลาด (ไม่บังคับ)</label>
        <div className="modal-foot">
          <button type="button" className="btn btn-quiet" onClick={onClose}>ยกเลิก</button>
          <button className="btn btn-primary" disabled={busy || !f.display_name.trim() || !f.consent_general}>{busy ? 'กำลังบันทึก…' : 'เพิ่มลีด'}</button>
        </div>
      </form>
    </Modal>
  )
}
