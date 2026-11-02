import { useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { api } from '../api.js'
import AuthFrame from './AuthFrame.jsx'

// Opened from the invite / forgot-password link: /set-password?token=...
export default function SetPassword() {
  const [params] = useSearchParams()
  const token = params.get('token') || ''
  const [pw, setPw] = useState('')
  const [pw2, setPw2] = useState('')
  const [error, setError] = useState('')
  const [done, setDone] = useState(false)
  const [busy, setBusy] = useState(false)

  const tooShort = pw.length > 0 && pw.length < 8
  const mismatch = pw2.length > 0 && pw !== pw2

  async function submit(e) {
    e.preventDefault()
    setError(''); setBusy(true)
    try {
      await api('POST', '/auth/set-password', { token, new_password: pw })
      setDone(true)
    } catch (err) { setError(err.message) } finally { setBusy(false) }
  }

  if (!token) {
    return (
      <AuthFrame title="ไม่พบลิงก์ตั้งรหัสผ่าน">
        <div className="alert alert-error">ลิงก์ไม่สมบูรณ์ เปิดจากลิงก์ที่แอดมินส่งให้อีกครั้ง หรือขอลิงก์ใหม่</div>
        <Link to="/login" className="btn btn-quiet">ไปหน้าเข้าสู่ระบบ</Link>
      </AuthFrame>
    )
  }
  if (done) {
    return (
      <AuthFrame title="ตั้งรหัสผ่านเรียบร้อย">
        <p>ใช้รหัสผ่านใหม่เข้าสู่ระบบได้เลย</p>
        <Link to="/login" className="btn btn-primary">เข้าสู่ระบบ</Link>
      </AuthFrame>
    )
  }
  return (
    <AuthFrame title="ตั้งรหัสผ่าน" intro="ตั้งรหัสผ่านของคุณเอง ลิงก์นี้ใช้ได้ครั้งเดียว">
      <form onSubmit={submit} className="form" noValidate>
        {error && <div className="alert alert-error" role="alert">{error}</div>}
        <label className="field">
          <span>รหัสผ่านใหม่</span>
          <input type="password" autoComplete="new-password" value={pw} onChange={(e) => setPw(e.target.value)} aria-invalid={tooShort} autoFocus />
          <small className={tooShort ? 'err' : ''}>ยาว 8–72 ตัวอักษร</small>
        </label>
        <label className="field">
          <span>ยืนยันรหัสผ่านใหม่</span>
          <input type="password" autoComplete="new-password" value={pw2} onChange={(e) => setPw2(e.target.value)} aria-invalid={mismatch} />
          {mismatch && <small className="err">รหัสผ่านสองช่องไม่ตรงกัน</small>}
        </label>
        <button className="btn btn-primary btn-block" disabled={busy || pw.length < 8 || pw.length > 72 || pw !== pw2}>{busy ? 'กำลังบันทึก…' : 'ตั้งรหัสผ่าน'}</button>
      </form>
    </AuthFrame>
  )
}
