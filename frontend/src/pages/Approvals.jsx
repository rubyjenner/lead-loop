import { useCallback, useEffect, useState } from 'react'
import { api } from '../api.js'
import { useAuth } from '../auth.jsx'
import { Toast } from '../ui.jsx'
import ContentCard from './ContentCard.jsx'

// "ถูกปฏิเสธ"     = rejected by somebody else (the creator has to fix it)
// "ที่ฉันปฏิเสธ"  = rejected by me -> waiting for the creator, nothing for me to do
const TABS = [
  ['pending_approval', 'รออนุมัติ'],
  ['draft', 'ร่าง'],
  ['approved', 'อนุมัติแล้ว'],
  ['rejected', 'ถูกปฏิเสธ'],
  ['rejected_by_me', 'ที่ฉันปฏิเสธ'],
]

const lc = (s) => (s || '').toLowerCase()

export default function Approvals() {
  const { user } = useAuth()
  const [tab, setTab] = useState('pending_approval')
  const [all, setAll] = useState(null)
  const [error, setError] = useState('')
  const [rowError, setRowError] = useState('')
  const [toast, setToast] = useState('')

  const load = useCallback(async () => {
    try { setAll(await api('GET', '/contents')); setError('') } catch (e) { setError(e.message) }
  }, [])
  useEffect(() => { load() }, [load])

  const me = lc(user.email)
  const inTab = (c, k) => {
    if (k === 'rejected') return c.status === 'rejected' && lc(c.rejected_by) !== me
    if (k === 'rejected_by_me') return c.status === 'rejected' && lc(c.rejected_by) === me
    return c.status === k
  }
  const count = (k) => (all || []).filter((c) => inTab(c, k)).length
  const shown = (all || []).filter((c) => inTab(c, tab))
  // sales never approve or reject, so they do not get the "rejected by me" tab
  const tabs = user.role === 'sales' ? TABS.filter(([k]) => k !== 'rejected_by_me') : TABS

  return (
    <div className="page">
      <header className="page-head">
        <div>
          <h1>อนุมัติคอนเทนต์</h1>
          <p className="muted">คอนเทนต์ที่ AI หรือทีมร่างต้องผ่านการอนุมัติก่อนนำไปใช้ ผู้อนุมัติต้องเป็นคนละคนกับผู้สร้าง ทำได้แค่อนุมัติหรือปฏิเสธ และการปฏิเสธต้องบอกเหตุผล</p>
        </div>
      </header>
      {error && <div className="alert alert-error" role="alert">{error}</div>}

      <div className="toolbar">
        <div className="seg" role="group" aria-label="กรองตามสถานะ">
          {tabs.map(([k, label]) => (
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
      {rowError && <div className="alert alert-error" role="alert">{rowError}</div>}
      <ul className="cards">
        {shown.map((c) => (
          <ContentCard key={c.id} c={c} image={c.product_image_url} campaignCreator={c.campaign_created_by} user={user} showCampaign reviewOnly
            onError={setRowError} onChanged={async (msg) => { if (msg) setToast(msg); await load() }} />
        ))}
      </ul>

      <Toast msg={toast} onDone={() => setToast('')} />
    </div>
  )
}