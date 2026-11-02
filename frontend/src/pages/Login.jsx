import { useState } from 'react'
import { Navigate, useLocation, useNavigate } from 'react-router-dom'
import { useAuth } from '../auth.jsx'
import AuthFrame from './AuthFrame.jsx'

export default function Login() {
  const { user, login, notice } = useAuth()
  const nav = useNavigate()
  const loc = useLocation()
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  if (user && !user.mustChange) return <Navigate to="/" replace />

  async function submit(e) {
    e.preventDefault()
    setError(''); setBusy(true)
    try {
      const r = await login(email.trim(), password)
      nav(r.must_change_password ? '/change-password' : loc.state?.from || '/', { replace: true })
    } catch (err) {
      setError(err.message)
    } finally { setBusy(false) }
  }

  return (
    <AuthFrame title="เข้าสู่ระบบ" intro="ใช้อีเมลและรหัสผ่านที่แอดมินสร้างให้ ถ้ายังไม่มีบัญชี ขอลิงก์เชิญจากแอดมิน">
      <form onSubmit={submit} className="form" noValidate>
        {notice && <div className="alert alert-info" role="status">{notice}</div>}
        {error && <div className="alert alert-error" role="alert">{error}</div>}
        <label className="field">
          <span>อีเมล</span>
          <input type="email" autoComplete="username" value={email} onChange={(e) => setEmail(e.target.value)} required autoFocus />
        </label>
        <label className="field">
          <span>รหัสผ่าน</span>
          <input type="password" autoComplete="current-password" value={password} onChange={(e) => setPassword(e.target.value)} required />
        </label>
        <button className="btn btn-primary btn-block" disabled={busy || !email || !password}>{busy ? 'กำลังเข้าสู่ระบบ…' : 'เข้าสู่ระบบ'}</button>
        <p className="hint">ลืมรหัสผ่าน ให้แอดมินสร้างลิงก์ตั้งรหัสผ่านใหม่ให้</p>
      </form>
    </AuthFrame>
  )
}
