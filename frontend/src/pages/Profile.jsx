import { useState } from 'react'
import { Link } from 'react-router-dom'
import { api } from '../api.js'
import { useAuth } from '../auth.jsx'
import { useLang } from '../i18n.jsx'
import { ROLES, Toast, ThemeToggle } from '../ui.jsx'

export default function Profile() {
  const { user, setUser } = useAuth()
  const { lang, setLang, t } = useLang()
  const [name, setName] = useState(user.name || '')
  const [title, setTitle] = useState(user.job_title || '')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [toast, setToast] = useState('')

  async function save(e) {
    e.preventDefault(); setError(''); setBusy(true)
    try {
      await api('PUT', '/auth/me', { name: name.trim(), job_title: title.trim() })
      setUser({ ...user, name: name.trim(), job_title: title.trim() })
      setToast(t('profile.saved', 'บันทึกแล้ว'))
    } catch (err) { setError(err.message) } finally { setBusy(false) }
  }

  return (
    <div className="page">
      <header className="page-head"><div><h1>{t('profile.title', 'โปรไฟล์และการตั้งค่า')}</h1><p className="muted">{t('profile.sub', 'ชื่อและตำแหน่งของคุณจะแสดงแทนอีเมลในแคมเปญและหน้าอนุมัติ')}</p></div></header>
      <section className="panel narrow">
        <form className="form" onSubmit={save} noValidate>
          {error && <div className="alert alert-error" role="alert">{error}</div>}
          <dl className="facts">
            <div><dt>{t('profile.email', 'อีเมล')}</dt><dd>{user.email}</dd></div>
            <div><dt>{t('profile.role', 'บทบาท')}</dt><dd>{ROLES[user.role]?.label || user.role}</dd></div>
          </dl>
          <label className="field"><span>{t('profile.name', 'ชื่อ-สกุล')}</span><input value={name} maxLength={100} onChange={(e) => setName(e.target.value)} /></label>
          <label className="field"><span>{t('profile.job', 'ตำแหน่ง')}</span><input value={title} maxLength={100} onChange={(e) => setTitle(e.target.value)} placeholder="เช่น Marketing Manager" /></label>
          <div className="modal-foot" style={{ justifyContent: 'flex-start' }}>
            <button className="btn btn-primary" disabled={busy || !name.trim()}>{t('profile.save', 'บันทึก')}</button>
            <Link className="btn btn-quiet" to="/change-password">{t('profile.password', 'เปลี่ยนรหัสผ่าน')}</Link>
          </div>
        </form>
      </section>
      <section className="panel narrow" style={{ marginTop: 16 }}>
        <h2>{t('profile.appearance', 'การแสดงผล')}</h2>
        <div className="pref-row"><span>{t('profile.theme', 'ธีม')}</span><ThemeToggle /></div>
        <div className="pref-row"><span>{t('profile.lang', 'ภาษา')}</span>
          <div className="seg seg-sm" role="group"><button className={lang === 'th' ? 'seg-on' : ''} onClick={() => setLang('th')}>ไทย</button><button className={lang === 'en' ? 'seg-on' : ''} onClick={() => setLang('en')}>English</button></div>
        </div>
        {lang === 'en' && <p className="muted small">{t('profile.note', 'ตอนนี้แปลเฉพาะเมนูและหน้านี้')}</p>}
      </section>
      <Toast msg={toast} onDone={() => setToast('')} />
    </div>
  )
}
