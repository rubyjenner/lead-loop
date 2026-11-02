import { useState } from 'react'
import { Navigate, useNavigate } from 'react-router-dom'
import { api, setToken } from '../api.js'
import { useAuth } from '../auth.jsx'
import AuthFrame from './AuthFrame.jsx'

// Shown at first login with a temporary password (must_change_password).
export default function ChangePassword() {
  const { user, setUser } = useAuth()
  const nav = useNavigate()
  const [cur, setCur] = useState('')
  const [pw, setPw] = useState('')
  const [pw2, setPw2] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  if (!user) return <Navigate to="/login" replace />

  async function submit(e) {
    e.preventDefault()
    setError(''); setBusy(true)
    try {
      const r = await api('POST', '/auth/password', { current_password: cur, new_password: pw })
      setToken(r.token)
      setUser({ ...user, mustChange: false })
      nav('/', { replace: true })
    } catch (err) { setError(err.message) } finally { setBusy(false) }
  }

  return (
    <AuthFrame title="เปลี่ยนรหัสผ่าน" intro="บัญชีนี้ใช้รหัสผ่านชั่วคราว ตั้งรหัสผ่านของคุณเองก่อนเริ่มใช้งาน">
      <form onSubmit={submit} className="form" noValidate>
        {error && <div className="alert alert-error" role="alert">{error}</div>}
        <label className="field"><span>รหัสผ่านปัจจุบัน</span>
          <input type="password" autoComplete="current-password" value={cur} onChange={(e) => setCur(e.target.value)} autoFocus />
        </label>
        <label className="field"><span>รหัสผ่านใหม่</span>
          <input type="password" autoComplete="new-password" value={pw} onChange={(e) => setPw(e.target.value)} />
          <small>ยาว 8–72 ตัวอักษร และต้องไม่ซ้ำกับรหัสเดิม</small>
        </label>
        <label className="field"><span>ยืนยันรหัสผ่านใหม่</span>
          <input type="password" autoComplete="new-password" value={pw2} onChange={(e) => setPw2(e.target.value)} />
          {pw2 && pw !== pw2 && <small className="err">รหัสผ่านสองช่องไม่ตรงกัน</small>}
        </label>
        <button className="btn btn-primary btn-block" disabled={busy || !cur || pw.length < 8 || pw !== pw2}>{busy ? 'กำลังบันทึก…' : 'เปลี่ยนรหัสผ่าน'}</button>
      </form>
    </AuthFrame>
  )
}
