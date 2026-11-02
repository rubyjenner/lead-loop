import { useEffect, useState } from 'react'
import { api } from '../api.js'
import { fmtDateTime } from '../ui.jsx'

// Plain-language names for audit actions.
const ACTIONS = {
  login_success: 'ล็อกอินสำเร็จ', login_failed: 'ล็อกอินไม่สำเร็จ',
  user_created: 'สร้างบัญชี', user_updated: 'แก้ไขบัญชี',
  password_changed: 'เปลี่ยนรหัสผ่าน', password_reset: 'แอดมินตั้งรหัสผ่านให้',
  reset_link_created: 'สร้างลิงก์ตั้งรหัสผ่าน', password_set_via_link: 'ตั้งรหัสผ่านจากลิงก์', set_password_failed: 'ใช้ลิงก์ไม่สำเร็จ',
  content_approved: 'อนุมัติคอนเทนต์', content_approve_denied: 'ถูกปฏิเสธการอนุมัติ (เป็นผู้สร้างเอง)',
  content_submitted: 'ส่งคอนเทนต์ขออนุมัติ', content_rejected: 'ปฏิเสธคอนเทนต์',
  lead_created: 'สร้างลีด', lead_consent_granted: 'ลีดให้ความยินยอม',
}
const BAD = new Set(['login_failed', 'content_approve_denied', 'set_password_failed'])

export default function Audit() {
  const [rows, setRows] = useState(null)
  const [error, setError] = useState('')
  useEffect(() => { api('GET', '/audit-log?limit=100').then(setRows).catch((e) => setError(e.message)) }, [])
  return (
    <div className="page">
      <header className="page-head">
        <div><h1>บันทึกการใช้งาน</h1><p className="muted">ใครทำอะไร เมื่อไร (ไม่เก็บรหัสผ่านหรือ token) แสดง 100 รายการล่าสุด</p></div>
      </header>
      {error && <div className="alert alert-error">{error}</div>}
      {!rows && !error && <div className="muted">กำลังโหลด…</div>}
      {rows && (
        <div className="table-wrap">
          <table className="table">
            <thead><tr><th>เวลา</th><th>ผู้ทำ</th><th>การกระทำ</th><th>เป้าหมาย</th><th>รายละเอียด</th></tr></thead>
            <tbody>
              {rows.map((r) => (
                <tr key={r.id}>
                  <td className="nowrap">{fmtDateTime(r.at)}</td>
                  <td>{r.actor_email || <span className="muted">ไม่ระบุ</span>}</td>
                  <td><span className={BAD.has(r.action) ? 'tag tag-warn' : 'tag'}>{ACTIONS[r.action] || r.action}</span></td>
                  <td className="muted">{r.target}</td>
                  <td className="muted">{r.detail}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}
