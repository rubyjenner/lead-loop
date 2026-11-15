import { useCallback, useEffect, useState } from 'react'
import { useParams } from 'react-router-dom'
import QRCode from 'qrcode'
import './payments.css'

// Public page (no login): the customer opens the link sales sent, sees the amount and scans the QR with a banking app.
// The QR image is drawn here in the browser from the payload text, so nothing is sent to another site.
// With the PromptPay QR used now there is no automatic confirmation: staff mark the payment as paid, and this page
// shows "paid" on its next refresh (it checks every 5 seconds while the payment is open).

// If your other public pages call the API with another base path, change it here (same value as src/api.js uses).
const BASE = '/api'

async function publicCall(method, path) {
  const res = await fetch(BASE + path, { method, headers: { Accept: 'application/json' } })
  let body = null
  try { body = await res.json() } catch { /* not JSON */ }
  return { status: res.status, body }
}

const baht = (v) => Number(v).toLocaleString('th-TH', { minimumFractionDigits: 2, maximumFractionDigits: 2 })
const when = (iso) => new Date(iso).toLocaleString('th-TH', { dateStyle: 'medium', timeStyle: 'short' })

export default function Pay() {
  const { token } = useParams()
  const [pay, setPay] = useState(null)
  const [state, setState] = useState('loading') // loading | ok | notfound | error
  const [qr, setQr] = useState('')
  const [busy, setBusy] = useState(false)
  const [msg, setMsg] = useState('')

  const load = useCallback(async () => {
    try {
      const r = await publicCall('GET', `/public/pay/${encodeURIComponent(token)}`)
      if (r.status === 404) { setState('notfound'); return }
      if (r.status !== 200 || !r.body) { setState('error'); return }
      setPay(r.body); setState('ok')
    } catch { setState('error') }
  }, [token])

  useEffect(() => { load() }, [load])

  // keep checking while the payment is open, so the page turns to "paid" without a manual refresh
  useEffect(() => {
    if (state !== 'ok' || pay?.status !== 'pending') return undefined
    const t = setInterval(load, 5000)
    return () => clearInterval(t)
  }, [state, pay?.status, load])

  useEffect(() => {
    let cancelled = false
    if (pay?.status === 'pending' && pay.qr_payload) {
      QRCode.toDataURL(pay.qr_payload, { margin: 2, width: 320, errorCorrectionLevel: 'M' })
        .then((url) => { if (!cancelled) setQr(url) })
        .catch(() => { if (!cancelled) setQr('') })
    } else {
      setQr('')
    }
    return () => { cancelled = true }
  }, [pay?.status, pay?.qr_payload])

  async function mockPay() {
    setBusy(true); setMsg('')
    try {
      const r = await publicCall('POST', `/public/pay/${encodeURIComponent(token)}/mock-pay`)
      if (r.status !== 200) setMsg((r.body && r.body.error) || 'ทำรายการไม่สำเร็จ')
      await load()
    } catch { setMsg('เชื่อมต่อไม่ได้') } finally { setBusy(false) }
  }

  if (state === 'loading') return <div className="pay-page"><p className="muted">กำลังโหลด…</p></div>
  if (state === 'notfound') {
    return (
      <div className="pay-page">
        <div className="pay-card"><h1>ไม่พบรายการชำระเงิน</h1>
          <p className="muted">ลิงก์อาจไม่ถูกต้องหรือหมดอายุ ติดต่อเจ้าหน้าที่ที่ส่งลิงก์ให้คุณ</p></div>
      </div>
    )
  }
  if (state === 'error') {
    return (
      <div className="pay-page">
        <div className="pay-card"><h1>เปิดหน้านี้ไม่ได้ในขณะนี้</h1>
          <p className="muted">ลองใหม่อีกครั้งในอีกสักครู่</p>
          <button className="btn" onClick={load}>ลองใหม่</button></div>
      </div>
    )
  }

  return (
    <div className="pay-page">
      <div className="pay-card">
        <h1>ชำระเงิน</h1>
        {pay.description && <p className="pay-desc">{pay.description}</p>}
        <p className="pay-amount">{baht(pay.amount)} <span>บาท</span></p>
        <p className="pay-ref muted small">รหัสอ้างอิง {pay.reference}</p>

        {pay.status === 'pending' && (
          <>
            {qr
              ? <img className="pay-qr" src={qr} alt="QR PromptPay สำหรับชำระเงิน" />
              : <p className="muted">กำลังสร้าง QR…</p>}
            <ol className="pay-steps small">
              <li>เปิดแอปธนาคารแล้วเลือกสแกน QR</li>
              <li>ตรวจยอด {baht(pay.amount)} บาทและชื่อผู้รับก่อนกดยืนยัน</li>
              <li>เมื่อเจ้าหน้าที่ตรวจพบการโอน หน้านี้จะแสดงว่าชำระแล้ว (ใช้เวลาสักครู่)</li>
            </ol>
            <p className="muted small">ชำระได้ก่อน {when(pay.expires_at)}</p>
            {pay.mock_pay && (
              <div className="pay-mock">
                <p className="small">โหมดทดสอบ: ปุ่มนี้จำลองว่าลูกค้าจ่ายเงินแล้ว</p>
                <button className="btn" onClick={mockPay} disabled={busy}>จำลองการจ่ายเงิน</button>
                {msg && <p className="pay-err small">{msg}</p>}
              </div>
            )}
          </>
        )}

        {pay.status === 'paid' && (
          <div className="pay-state pay-ok"><strong>ชำระเงินแล้ว ขอบคุณค่ะ</strong>
            <p className="small">เจ้าหน้าที่จะติดต่อกลับเรื่องขั้นตอนต่อไป</p></div>
        )}
        {pay.status === 'expired' && (
          <div className="pay-state"><strong>รายการนี้หมดเวลาแล้ว</strong>
            <p className="small">หากยังไม่ได้โอน กรุณาติดต่อเจ้าหน้าที่เพื่อขอลิงก์ใหม่ ถ้าโอนไปแล้วแจ้งเจ้าหน้าที่พร้อมรหัสอ้างอิงด้านบน</p></div>
        )}
        {pay.status === 'cancelled' && (
          <div className="pay-state"><strong>รายการนี้ถูกยกเลิก</strong>
            <p className="small">กรุณาอย่าโอนเงินตาม QR นี้ ติดต่อเจ้าหน้าที่หากมีข้อสงสัย</p></div>
        )}
      </div>
    </div>
  )
}