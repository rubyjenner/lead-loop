import { useCallback, useEffect, useMemo, useState } from 'react'
import { api } from '../api.js'
import { useAuth } from '../auth.jsx'
import { CopyField, Modal, ROLES, Toast, fmtDateTime, timeAgo } from '../ui.jsx'

function statusOf(u) {
  if (!u.active) return { key: 'off', label: 'ปิดใช้งาน' }
  if (u.pending_link) return { key: 'wait', label: 'รอตั้งรหัสผ่าน' }
  if (u.must_change_password) return { key: 'wait', label: 'ต้องเปลี่ยนรหัสผ่าน' }
  return { key: 'on', label: 'ใช้งานอยู่' }
}

function Person({ u }) {
  const label = (u.name || u.email).trim().slice(0, 1).toUpperCase()
  return (
    <div className="person">
      <div className={`avatar role-${u.role}`} aria-hidden="true">{label}</div>
      <div>
        <div className="person-name">{u.name || <span className="muted">ยังไม่ตั้งชื่อ</span>}</div>
        <div className="muted small">{u.email}</div>
      </div>
    </div>
  )
}

export default function Users() {
  const { user: me } = useAuth()
  const [users, setUsers] = useState(null)
  const [check, setCheck] = useState(null)
  const [error, setError] = useState('')
  const [q, setQ] = useState('')
  const [roleFilter, setRoleFilter] = useState('all')
  const [modal, setModal] = useState(null) // {type:'invite'|'edit'|'link'|'toggle', user?, link?}
  const [toast, setToast] = useState('')

  const load = useCallback(async () => {
    try {
      const [u, c] = await Promise.all([api('GET', '/users'), api('GET', '/users/approver-check')])
      setUsers(u); setCheck(c); setError('')
    } catch (e) { setError(e.message) }
  }, [])
  useEffect(() => { load() }, [load])

  const shown = useMemo(() => {
    const s = q.trim().toLowerCase()
    return (users || []).filter((u) =>
      (roleFilter === 'all' || (roleFilter === 'off' ? !u.active : u.role === roleFilter && u.active)) &&
      (!s || u.email.toLowerCase().includes(s) || (u.name || '').toLowerCase().includes(s)))
  }, [users, q, roleFilter])

  const count = (f) => (users || []).filter(f).length
  const filters = [
    ['all', 'ทั้งหมด', count(() => true)],
    ['admin', 'แอดมิน', count((u) => u.role === 'admin' && u.active)],
    ['marketing', 'การตลาด', count((u) => u.role === 'marketing' && u.active)],
    ['sales', 'เซลส์', count((u) => u.role === 'sales' && u.active)],
    ['off', 'ปิดใช้งาน', count((u) => !u.active)],
  ]

  async function makeLink(u) {
    try {
      const r = await api('POST', `/users/${u.id}/reset-link`)
      setModal({ type: 'link', user: u, link: r.reset_url, expires: r.expires_at, kind: 'reset' })
      load()
    } catch (e) { setToast(e.message) }
  }

  return (
    <div className="page">
      <header className="page-head">
        <div>
          <h1>ผู้ใช้</h1>
          <p className="muted">บัญชีสร้างโดยแอดมินเท่านั้น กำหนดบทบาทตามหน้าที่ แล้วคัดลอกลิงก์ส่งให้เจ้าของบัญชีเอง</p>
        </div>
        <button className="btn btn-primary" onClick={() => setModal({ type: 'invite' })}>เชิญผู้ใช้</button>
      </header>

      {check && !check.ok && (
        <div className="alert alert-warn" role="status">
          <strong>ผู้อนุมัติไม่พอ</strong>
          <span>ตอนนี้มีผู้ใช้การตลาดหรือแอดมินที่ใช้งานอยู่ {check.active_approvers} คน ต้องมีอย่างน้อย {check.minimum} คน เพราะคนอนุมัติต้องไม่ใช่คนสร้างคอนเทนต์ ถ้ามีคนเดียวจะอนุมัติงานของตัวเองไม่ได้</span>
        </div>
      )}
      {error && <div className="alert alert-error" role="alert">{error}</div>}

      <div className="toolbar">
        <div className="seg" role="group" aria-label="กรองตามบทบาท">
          {filters.map(([k, label, n]) => (
            <button key={k} className={roleFilter === k ? 'seg-on' : ''} aria-pressed={roleFilter === k} onClick={() => setRoleFilter(k)}>
              {label}<span className="seg-n">{users ? n : ''}</span>
            </button>
          ))}
        </div>
        <input className="search" type="search" placeholder="ค้นหาชื่อหรืออีเมล" value={q} onChange={(e) => setQ(e.target.value)} aria-label="ค้นหาผู้ใช้" />
      </div>

      {!users && !error && <div className="muted pad">กำลังโหลด…</div>}
      {users && (
        <div className="table-wrap">
          <table className="table">
            <thead><tr><th>ผู้ใช้</th><th>บทบาท</th><th>สถานะ</th><th>เข้าใช้ล่าสุด</th><th className="right">จัดการ</th></tr></thead>
            <tbody>
              {shown.map((u) => {
                const st = statusOf(u)
                const self = String(u.id) === String(me.id)
                return (
                  <tr key={u.id} className={u.active ? '' : 'row-off'}>
                    <td><Person u={u} /></td>
                    <td><span className={`chip chip-${u.role}`}>{ROLES[u.role]?.label}</span></td>
                    <td><span className={`state state-${st.key}`}>{st.label}</span></td>
                    <td title={u.last_login_at ? fmtDateTime(u.last_login_at) : ''}>{timeAgo(u.last_login_at) || <span className="muted">ยังไม่เคยเข้า</span>}</td>
                    <td className="right actions">
                      <button className="btn btn-quiet" onClick={() => setModal({ type: 'edit', user: u })}>แก้ไข</button>
                      {u.active && <button className="btn btn-quiet" onClick={() => makeLink(u)}>ลิงก์ตั้งรหัสผ่าน</button>}
                      {!self && <button className="btn btn-quiet btn-danger" onClick={() => setModal({ type: 'toggle', user: u })}>{u.active ? 'ปิดใช้งาน' : 'เปิดใช้งาน'}</button>}
                    </td>
                  </tr>
                )
              })}
              {shown.length === 0 && <tr><td colSpan="5" className="empty-row">ไม่พบผู้ใช้ที่ตรงกับตัวกรอง</td></tr>}
            </tbody>
          </table>
        </div>
      )}

      {modal?.type === 'invite' && <InviteModal onClose={() => setModal(null)} onCreated={load} />}
      {modal?.type === 'edit' && <EditModal user={modal.user} isSelf={String(modal.user.id) === String(me.id)} onClose={() => setModal(null)} onSaved={() => { setModal(null); setToast('บันทึกแล้ว'); load() }} />}
      {modal?.type === 'toggle' && <ToggleModal user={modal.user} onClose={() => setModal(null)} onDone={(msg) => { setModal(null); setToast(msg); load() }} />}
      {modal?.type === 'link' && <LinkModal {...modal} onClose={() => setModal(null)} />}
      <Toast msg={toast} onDone={() => setToast('')} />
    </div>
  )
}

function InviteModal({ onClose, onCreated }) {
  const [f, setF] = useState({ name: '', email: '', role: 'marketing' })
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [result, setResult] = useState(null)
  const set = (k) => (e) => setF({ ...f, [k]: e.target.value })

  async function submit(e) {
    e.preventDefault()
    setError(''); setBusy(true)
    try {
      const r = await api('POST', '/users', { name: f.name.trim(), email: f.email.trim(), role: f.role })
      setResult(r); onCreated()
    } catch (err) { setError(err.message) } finally { setBusy(false) }
  }

  if (result) {
    return (
      <Modal title="สร้างบัญชีแล้ว" onClose={onClose}>
        <p>ส่งลิงก์นี้ให้ <strong>{result.name || result.email}</strong> เพื่อให้เขาตั้งรหัสผ่านเอง</p>
        <CopyField value={result.invite_url} />
        <p className="hint">ใช้ได้ครั้งเดียว ภายใน 72 ชั่วโมง (ถึง {fmtDateTime(result.invite_expires_at)}) ลิงก์จะไม่แสดงอีก ถ้าหาย ให้กด “ลิงก์ตั้งรหัสผ่าน” เพื่อสร้างใหม่</p>
        <div className="modal-foot"><button className="btn btn-primary" onClick={onClose}>เสร็จแล้ว</button></div>
      </Modal>
    )
  }
  return (
    <Modal title="เชิญผู้ใช้" onClose={onClose}>
      <form className="form" onSubmit={submit} noValidate>
        {error && <div className="alert alert-error" role="alert">{error}</div>}
        <label className="field"><span>ชื่อที่แสดง</span><input value={f.name} onChange={set('name')} maxLength={100} placeholder="เช่น ณภัทร มั่นคง" /></label>
        <label className="field"><span>อีเมล</span><input type="email" value={f.email} onChange={set('email')} placeholder="name@company.com" /></label>
        <fieldset className="field roles">
          <legend>บทบาท</legend>
          {Object.entries(ROLES).map(([k, r]) => (
            <label key={k} className={`role-opt${f.role === k ? ' role-on' : ''}`}>
              <input type="radio" name="role" value={k} checked={f.role === k} onChange={set('role')} />
              <span><strong>{r.label}</strong><small>{r.help}</small></span>
            </label>
          ))}
        </fieldset>
        <div className="modal-foot">
          <button type="button" className="btn btn-quiet" onClick={onClose}>ยกเลิก</button>
          <button className="btn btn-primary" disabled={busy || !f.email.includes('@')}>{busy ? 'กำลังสร้าง…' : 'สร้างบัญชีและลิงก์เชิญ'}</button>
        </div>
      </form>
    </Modal>
  )
}

function EditModal({ user, isSelf, onClose, onSaved }) {
  const [name, setName] = useState(user.name || '')
  const [role, setRole] = useState(user.role)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  async function submit(e) {
    e.preventDefault()
    setError(''); setBusy(true)
    const body = {}
    if (name.trim() !== (user.name || '')) body.name = name.trim()
    if (role !== user.role) body.role = role
    if (!Object.keys(body).length) { onClose(); return }
    try { await api('PATCH', `/users/${user.id}`, body); onSaved() } catch (err) { setError(err.message) } finally { setBusy(false) }
  }
  return (
    <Modal title="แก้ไขผู้ใช้" onClose={onClose}>
      <form className="form" onSubmit={submit}>
        {error && <div className="alert alert-error" role="alert">{error}</div>}
        <div className="muted small">{user.email}</div>
        <label className="field"><span>ชื่อที่แสดง</span><input value={name} onChange={(e) => setName(e.target.value)} maxLength={100} /></label>
        <label className="field"><span>บทบาท</span>
          <select value={role} onChange={(e) => setRole(e.target.value)}>
            {Object.entries(ROLES).map(([k, r]) => <option key={k} value={k}>{r.label}</option>)}
          </select>
          <small>{ROLES[role].help}</small>
        </label>
        {isSelf && role !== user.role && <div className="alert alert-warn">คุณกำลังเปลี่ยนบทบาทของตัวเอง เมนูบางอย่างจะหายไปทันที</div>}
        <div className="modal-foot">
          <button type="button" className="btn btn-quiet" onClick={onClose}>ยกเลิก</button>
          <button className="btn btn-primary" disabled={busy}>{busy ? 'กำลังบันทึก…' : 'บันทึก'}</button>
        </div>
      </form>
    </Modal>
  )
}

function ToggleModal({ user, onClose, onDone }) {
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const off = user.active
  async function go() {
    setBusy(true); setError('')
    try { await api('PATCH', `/users/${user.id}`, { active: !off }); onDone(off ? 'ปิดใช้งานแล้ว' : 'เปิดใช้งานแล้ว') } catch (e) { setError(e.message); setBusy(false) }
  }
  return (
    <Modal title={off ? 'ปิดใช้งานบัญชี' : 'เปิดใช้งานบัญชี'} onClose={onClose}>
      {error && <div className="alert alert-error" role="alert">{error}</div>}
      <p>{off
        ? <>ปิดบัญชี <strong>{user.name || user.email}</strong> คนนี้จะเข้าระบบไม่ได้ทันที และเซสชันที่เปิดอยู่จะหมดอายุ ข้อมูลที่เคยสร้างยังอยู่ครบ</>
        : <>เปิดบัญชี <strong>{user.name || user.email}</strong> ให้กลับมาล็อกอินได้ด้วยรหัสผ่านเดิม</>}</p>
      <div className="modal-foot">
        <button className="btn btn-quiet" onClick={onClose}>ยกเลิก</button>
        <button className={`btn ${off ? 'btn-danger-solid' : 'btn-primary'}`} onClick={go} disabled={busy}>{off ? 'ปิดใช้งาน' : 'เปิดใช้งาน'}</button>
      </div>
    </Modal>
  )
}

function LinkModal({ user, link, expires, onClose }) {
  return (
    <Modal title="ลิงก์ตั้งรหัสผ่านใหม่" onClose={onClose}>
      <p>ส่งลิงก์นี้ให้ <strong>{user.name || user.email}</strong></p>
      <CopyField value={link} />
      <p className="hint">ใช้ได้ครั้งเดียว ภายใน 1 ชั่วโมง (ถึง {fmtDateTime(expires)}) รหัสผ่านเดิมยังใช้ได้จนกว่าเขาจะตั้งรหัสใหม่ และลิงก์เก่าของคนนี้ถูกยกเลิกแล้ว</p>
      <div className="modal-foot"><button className="btn btn-primary" onClick={onClose}>เสร็จแล้ว</button></div>
    </Modal>
  )
}
