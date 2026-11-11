import { useCallback, useEffect, useState } from 'react'
import { api } from '../api.js'
import { Toast, fmtDateTime } from '../ui.jsx'

// Admin page: how long personal data is kept before it is deleted automatically (PDPA).
export default function Retention() {
  const [d, setD] = useState(null)
  const [f, setF] = useState({ inactive_days: 0, withdrawn_days: 0 })
  const [error, setError] = useState('')
  const [toast, setToast] = useState('')
  const [busy, setBusy] = useState(false)

  const load = useCallback(async () => {
    try {
      const r = await api('GET', '/retention')
      setD(r); setF({ inactive_days: r.inactive_days, withdrawn_days: r.withdrawn_days }); setError('')
    } catch (e) { setError(e.message) }
  }, [])
  useEffect(() => { load() }, [load])

  async function save(e) {
    e.preventDefault(); setBusy(true); setError('')
    try {
      await api('PUT', '/retention', { inactive_days: Number(f.inactive_days) || 0, withdrawn_days: Number(f.withdrawn_days) || 0 })
      setToast('บันทึกแล้ว'); await load()
    } catch (err) { setError(err.message) } finally { setBusy(false) }
  }
  async function runNow() {
    const n = (d?.inactive_due || 0) + (d?.withdrawn_due || 0)
    if (!window.confirm(`จะลบข้อมูลระบุตัวตนของ ${n} ลีดทันที ย้อนกลับไม่ได้ ยืนยันหรือไม่?`)) return
    setBusy(true); setError('')
    try {
      const r = await api('POST', '/retention/run')
      setToast(`ลบแล้ว ${r.inactive_done + r.withdrawn_done} ลีด`); await load()
    } catch (err) { setError(err.message) } finally { setBusy(false) }
  }

  const off = !d || (d.inactive_days === 0 && d.withdrawn_days === 0)
  return (
    <div className="page">
      <header className="page-head">
        <div>
          <h1>ระยะเก็บข้อมูล</h1>
          <p className="muted">ตั้งเวลาให้ระบบลบข้อมูลระบุตัวตนของลีดเองเมื่อครบกำหนด (ชื่อ ช่องทางติดต่อ งบ เหตุการณ์ และข้อความแชต) คะแนน สถานะ และหลักฐานความยินยอมยังอยู่เพื่อทำรายงาน ระบบทำงานวันละครั้งหลัง 03:00 น.</p>
        </div>
      </header>
      {error && <div className="alert alert-error" role="alert">{error}</div>}
      <div className="alert alert-info">ตอนนี้{off ? 'ปิดอยู่' : 'เปิดใช้'} ระยะเวลาที่เหมาะสมต้องให้บริษัท (Deputy Manager) กำหนดตามนโยบาย PDPA ลีดที่เซลส์กำลังดูแลอยู่ (พร้อมส่งต่อ / ส่งต่อแล้ว) จะไม่ถูกลบ</div>
      <form className="panel form" onSubmit={save} noValidate style={{ maxWidth: 640 }}>
        <label className="field"><span>ลบเมื่อไม่มีความเคลื่อนไหวนานกี่วัน (0 = ปิด, ตั้งได้ 30 ถึง 3650)</span>
          <input type="number" min="0" max="3650" value={f.inactive_days} onChange={(e) => setF({ ...f, inactive_days: e.target.value })} /></label>
        <p className="muted small">นับจากกิจกรรมล่าสุดของลูกค้า (ยินยอม ถามข้อความ ตอบแชต) ไม่นับงานติดตามอัตโนมัติ{d && d.inactive_days > 0 ? ` ตอนนี้ครบกำหนด ${d.inactive_due} ลีด` : ''}</p>
        <label className="field"><span>ลบหลังถอนความยินยอมกี่วัน (0 = ปิด, ตั้งได้ 7 ถึง 3650)</span>
          <input type="number" min="0" max="3650" value={f.withdrawn_days} onChange={(e) => setF({ ...f, withdrawn_days: e.target.value })} /></label>
        <p className="muted small">ข้อความแชตที่เก็บไว้จะถูกลบทันทีที่ถอน ส่วนชื่อและข้อมูลอื่นรอตามกำหนดนี้{d && d.withdrawn_days > 0 ? ` ตอนนี้ครบกำหนด ${d.withdrawn_due} ลีด` : ''}</p>
        <div className="actions">
          <button className="btn btn-primary" disabled={busy}>{busy ? 'กำลังบันทึก…' : 'บันทึก'}</button>
          <button type="button" className="btn btn-quiet" disabled={busy || off || !d || (d.inactive_due + d.withdrawn_due) === 0} onClick={runNow}>ลบที่ครบกำหนดตอนนี้</button>
        </div>
      </form>
      {d?.last_run && <p className="muted small" style={{ marginTop: 12 }}>รอบล่าสุด {fmtDateTime(d.last_run.at)} ({d.last_run.by === 'schedule' ? 'ตามเวลา' : 'สั่งเอง'}) ลบ {d.last_run.inactive_done + d.last_run.withdrawn_done} ลีด</p>}
      <Toast msg={toast} onDone={() => setToast('')} />
    </div>
  )
}
