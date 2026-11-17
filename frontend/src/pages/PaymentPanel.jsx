import { useCallback, useEffect, useState } from 'react'
import { api } from '../api.js'
import './payments.css'

// Payment requests of one lead, for the lead detail page.
//   <PaymentPanel leadId={lead.id} leadStatus={lead.status} canCreate={role === 'sales' || role === 'admin'}
//                 canConfirm={role === 'marketing' || role === 'admin'} myEmail={me.email} onChanged={load} />
// onChanged (optional) runs after every successful action, so the page can reload the lead (paid -> status closed).
// canCreate / canConfirm / myEmail only decide which buttons are shown; the server enforces the real rules
// (roles, "the person who confirms must not be the creator", lead must be qualified or handed off).

const STATUS = { pending: 'รอชำระ', paid: 'ชำระแล้ว', expired: 'หมดเวลา', cancelled: 'ยกเลิก' }
const baht = (v) => Number(v).toLocaleString('th-TH', { minimumFractionDigits: 2, maximumFractionDigits: 2 })
const when = (iso) => (iso ? new Date(iso).toLocaleString('th-TH', { dateStyle: 'medium', timeStyle: 'short' }) : '')

export default function PaymentPanel({ leadId, leadStatus, canCreate = true, canConfirm = true, myEmail = '', onChanged }) {
  const [items, setItems] = useState([])
  const [error, setError] = useState('')
  const [note, setNote] = useState('')
  const [busy, setBusy] = useState(false)
  const [manual, setManual] = useState('') // optional negotiated amount; empty = product price

  const load = useCallback(async () => {
    try { setItems(await api('GET', `/payments?lead_id=${leadId}`)); setError('') } catch (e) { setError(e.message) }
  }, [leadId])
  useEffect(() => { load() }, [load])

  async function run(fn) {
    setBusy(true); setError(''); setNote('')
    try { await fn(); await load(); if (onChanged) onChanged() } catch (e) { setError(e.message) } finally { setBusy(false) }
  }

  const open = items.find((p) => p.status === 'pending')
  const eligible = leadStatus === 'qualified' || leadStatus === 'handed_off'

  const create = () => run(async () => {
    const body = {}
    if (manual.trim() !== '') {
      const v = Number(manual)
      if (!Number.isFinite(v) || v <= 0) throw new Error('ยอดต้องเป็นตัวเลขมากกว่า 0')
      body.amount = v
    }
    await api('POST', `/leads/${leadId}/payments`, body)
    setManual('')
    setNote('สร้างรายการแล้ว กด "คัดลอกลิงก์" เพื่อส่งให้ลูกค้า')
  })

  const copyLink = async (p) => {
    const url = window.location.origin + p.pay_path
    try { await navigator.clipboard.writeText(url); setNote('คัดลอกลิงก์แล้ว') } catch { setNote(url) }
  }

  const sendLink = (p) => {
    if (!window.confirm(`ส่งลิงก์ชำระเงิน ยอด ${baht(p.amount)} บาท ให้ลูกค้าทางแชต? ตรวจยอดให้ถูกต้องก่อนส่ง`)) return
    run(async () => {
      await api('POST', `/payments/${p.id}/send-link`)
      setNote('ส่งลิงก์ให้ลูกค้าทางแชตแล้ว')
    })
  }

  const cancel = (p) => {
    if (!window.confirm('ยกเลิกรายการชำระเงินนี้? ลูกค้าจะโอนตาม QR เดิมไม่ได้')) return
    run(() => api('POST', `/payments/${p.id}/cancel`))
  }

  const confirmPaid = (p) => {
    const ref = window.prompt('เลขอ้างอิงการโอนหรือเลขที่สลิป (ไม่บังคับ ปล่อยว่างได้)')
    if (ref === null) return
    if (!window.confirm('ยืนยันว่าเงินเข้าบัญชีแล้ว? ลีดจะเปลี่ยนเป็น "ปิดการขาย"')) return
    run(() => api('POST', `/payments/${p.id}/confirm`, { reference: ref.trim() }))
  }

  const mine = (p) => myEmail && p.created_by && p.created_by.toLowerCase() === myEmail.toLowerCase()

  return (
    <section className="panel" style={{ marginTop: 16 }}>
      <h2>การชำระเงิน</h2>
      {error && <div className="alert alert-error" role="alert">{error}</div>}
      {note && <p className="muted small">{note}</p>}

      {canCreate && !open && (
        eligible ? (
          <div className="pay-new">
            <label className="small">ยอด (บาท) เว้นว่าง = ราคาสินค้าของแคมเปญ
              <input className="pay-input" inputMode="decimal" value={manual} onChange={(e) => setManual(e.target.value)} placeholder="เช่น 11900" />
            </label>
            <button className="btn btn-primary" onClick={create} disabled={busy}>ขอเก็บเงิน</button>
          </div>
        ) : <p className="muted small">ขอเก็บเงินได้เมื่อลีดเป็น "พร้อมส่งต่อ" หรือ "ส่งต่อเซลส์แล้ว"</p>
      )}

      {items.length === 0 && !error && <p className="muted small">ยังไม่มีรายการชำระเงิน</p>}
      <ul className="pay-list">
        {items.map((p) => (
          <li key={p.id} className={`pay-row pay-${p.status}`}>
            <div>
              <strong>{baht(p.amount)} บาท</strong> · {STATUS[p.status] || p.status}
              {p.amount_source === 'manual' && <span className="tag">ยอดที่กรอกเอง</span>}
              <div className="muted small">
                {p.reference} · {p.description || 'ไม่มีชื่อสินค้า'} · สร้างโดย {p.created_by} · {when(p.created_at)}
                {p.status === 'pending' && <> · หมดเวลา {when(p.expires_at)}</>}
                {p.status === 'pending' && p.link_sent_at && <> · ส่งลิงก์ในแชตแล้ว {when(p.link_sent_at)}</>}
                {p.status === 'paid' && <> · ยืนยันโดย {p.confirmed_by} {when(p.paid_at)}{p.provider_ref ? ` · อ้างอิง ${p.provider_ref}` : ''}</>}
              </div>
            </div>
            {p.status === 'pending' && (
              <div className="pay-actions">
                {canCreate && p.can_send_link && (
                  <button className="btn btn-primary" onClick={() => sendLink(p)} disabled={busy}>
                    {p.link_sent_at ? 'ส่งลิงก์ในแชตอีกครั้ง' : 'ส่งลิงก์ในแชต'}
                  </button>
                )}
                <button className="btn btn-quiet" onClick={() => copyLink(p)}>คัดลอกลิงก์</button>
                <a className="btn btn-quiet" href={p.pay_path} target="_blank" rel="noreferrer">เปิดหน้าชำระเงิน</a>
                {canCreate && <button className="btn btn-quiet" onClick={() => cancel(p)} disabled={busy}>ยกเลิก</button>}
                {canConfirm && !mine(p) && <button className="btn btn-primary" onClick={() => confirmPaid(p)} disabled={busy}>ยืนยันว่าเงินเข้าแล้ว</button>}
                {canConfirm && mine(p) && <span className="muted small">ต้องให้คนอื่นเป็นผู้ยืนยัน</span>}
              </div>
            )}
          </li>
        ))}
      </ul>
    </section>
  )
}
