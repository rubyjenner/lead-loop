import { useCallback, useEffect, useState } from 'react'
import { api } from '../api.js'
import { useAuth } from '../auth.jsx'
import { CHANNELS, Modal, SOURCE, Toast, timeAgo } from '../ui.jsx'

const TABS = [
  ['pending_approval', 'รออนุมัติ'],
  ['draft', 'ร่าง'],
  ['approved', 'อนุมัติแล้ว'],
  ['rejected', 'ถูกปฏิเสธ'],
]

export default function Approvals() {
  const { user } = useAuth()
  const [tab, setTab] = useState('pending_approval')
  const [all, setAll] = useState(null)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(0)
  const [rowError, setRowError] = useState({})
  const [confirm, setConfirm] = useState(null) // content to reject
  const [toast, setToast] = useState('')

  const load = useCallback(async () => {
    try { setAll(await api('GET', '/contents')); setError('') } catch (e) { setError(e.message) }
  }, [])
  useEffect(() => { load() }, [load])

  const canAct = user.role === 'admin' || user.role === 'marketing'
  const me = user.email.toLowerCase()
  const count = (s) => (all || []).filter((c) => c.status === s).length
  const shown = (all || []).filter((c) => c.status === tab)

  async function act(c, action, okMsg) {
    setBusy(c.id); setRowError((r) => ({ ...r, [c.id]: '' }))
    try {
      await api('POST', `/contents/${c.id}/${action}`)
      setToast(okMsg)
      await load()
    } catch (e) {
      setRowError((r) => ({ ...r, [c.id]: e.message }))
      if (e.status === 409) load()
    } finally { setBusy(0) }
  }

  return (
    <div className="page">
      <header className="page-head">
        <div>
          <h1>อนุมัติคอนเทนต์</h1>
          <p className="muted">คอนเทนต์ที่ AI หรือทีมร่างต้องผ่านการอนุมัติก่อนนำไปใช้ และผู้อนุมัติต้องเป็นคนละคนกับผู้สร้าง</p>
        </div>
      </header>
      {error && <div className="alert alert-error" role="alert">{error}</div>}

      <div className="toolbar">
        <div className="seg" role="group" aria-label="กรองตามสถานะ">
          {TABS.map(([k, label]) => (
            <button key={k} className={tab === k ? 'seg-on' : ''} aria-pressed={tab === k} onClick={() => setTab(k)}>
              {label}<span className="seg-n">{all ? count(k) : ''}</span>
            </button>
          ))}
        </div>
      </div>

      {!all && !error && <div className="muted pad">กำลังโหลด…</div>}
      {all && shown.length === 0 && (
        <div className="empty">
          <p className="empty-title">{tab === 'pending_approval' ? 'ไม่มีคอนเทนต์ที่รออนุมัติ' : 'ยังไม่มีรายการในสถานะนี้'}</p>
          <p className="muted">{tab === 'pending_approval' ? 'เมื่อมีคนส่งคอนเทนต์ขออนุมัติ จะมาแสดงที่นี่' : 'ลองดูแท็บอื่น'}</p>
        </div>
      )}
      <ul className="cards">
        {shown.map((c) => {
          const mine = (c.created_by || '').toLowerCase() === me
          return (
            <li key={c.id} className="card">
              <div className="card-top">
                <span className="card-camp">{c.campaign_name}</span>
                <span className="tag">{CHANNELS[c.channel] || c.channel}</span>
                <span className={`src src-${c.generated_by}`}>{SOURCE[c.generated_by] || c.generated_by}</span>
              </div>
              <p className="card-body">{c.body}</p>
              <div className="card-meta muted small">
                สร้างโดย {mine ? 'คุณ' : c.created_by || 'ไม่ระบุ'} · {timeAgo(c.created_at)}
                {c.status === 'approved' && c.approved_by && <> · อนุมัติโดย {c.approved_by.toLowerCase() === me ? 'คุณ' : c.approved_by}</>}
              </div>
              {rowError[c.id] && <div className="alert alert-error" role="alert">{rowError[c.id]}</div>}
              {canAct && c.status === 'pending_approval' && (
                <div className="card-actions">
                  {mine ? (
                    <p className="own-note">คุณเป็นผู้สร้างคอนเทนต์นี้ จึงอนุมัติเองไม่ได้ ให้เพื่อนร่วมทีมที่เป็นการตลาดหรือแอดมินอนุมัติแทน</p>
                  ) : (
                    <>
                      <button className="btn btn-primary" disabled={busy === c.id} onClick={() => act(c, 'approve', 'อนุมัติแล้ว')}>อนุมัติ</button>
                      <button className="btn btn-quiet btn-danger" disabled={busy === c.id} onClick={() => setConfirm(c)}>ปฏิเสธ</button>
                    </>
                  )}
                </div>
              )}
              {canAct && (c.status === 'draft' || c.status === 'rejected') && (
                <div className="card-actions">
                  <button className="btn btn-primary" disabled={busy === c.id} onClick={() => act(c, 'submit', 'ส่งขออนุมัติแล้ว')}>
                    {c.status === 'rejected' ? 'ส่งขออนุมัติอีกครั้ง' : 'ส่งขออนุมัติ'}
                  </button>
                </div>
              )}
            </li>
          )
        })}
      </ul>

      {confirm && (
        <Modal title="ปฏิเสธคอนเทนต์นี้" onClose={() => setConfirm(null)}>
          <p className="card-body quote">{confirm.body}</p>
          <p>คอนเทนต์จะถูกส่งกลับให้ผู้สร้างแก้ไข และส่งขออนุมัติใหม่ได้</p>
          <div className="modal-foot">
            <button className="btn btn-quiet" onClick={() => setConfirm(null)}>ยกเลิก</button>
            <button className="btn btn-danger-solid" onClick={() => { const c = confirm; setConfirm(null); act(c, 'reject', 'ปฏิเสธแล้ว') }}>ปฏิเสธ</button>
          </div>
        </Modal>
      )}
      <Toast msg={toast} onDone={() => setToast('')} />
    </div>
  )
}
