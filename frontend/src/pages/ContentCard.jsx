import { useState } from 'react'
import { Link } from 'react-router-dom'
import { api } from '../api.js'
import { CHANNELS, CONTENT_STATUS, Modal, SOURCE, canEditCampaign, fmtDateTime, personLabel, timeAgo } from '../ui.jsx'

// One content draft, shown like a real ad: the product picture on top, the text under it.
// Who can do what (the server enforces the same rules, see backend/roles.go):
//   creator / admin  -> edit the text, submit for approval
//   another marketing/admin user -> approve or reject (a reason is required to reject)
//   sales -> view only
// props: c (content row), image (product picture url or ''), campaignCreator (email), onChanged(msg), onError(text)
export default function ContentCard({ c, image, campaignCreator, user, onChanged, onError, showCampaign }) {
  const [editing, setEditing] = useState(false)
  const [text, setText] = useState(c.body)
  const [busy, setBusy] = useState(false)
  const [rejecting, setRejecting] = useState(false)
  const [reason, setReason] = useState('')
  const [copied, setCopied] = useState(false)

  const me = (user.email || '').toLowerCase()
  const isCreator = (c.created_by || '').toLowerCase() === me || (campaignCreator || '').toLowerCase() === me
  const mayEdit = canEditCampaign(user, campaignCreator)
  const mayDecide = (user.role === 'admin' || user.role === 'marketing') && !isCreator

  async function run(fn, okMsg) {
    setBusy(true); onError('')
    try { await fn(); await onChanged(okMsg) } catch (e) { onError(e.message); if (e.status === 409) await onChanged('') } finally { setBusy(false) }
  }
  const save = () => run(async () => { await api('PUT', `/contents/${c.id}`, { body: text }); setEditing(false) },
    c.status === 'approved' || c.status === 'published' ? 'บันทึกแล้ว คอนเทนต์กลับไปรออนุมัติใหม่' : 'บันทึกแล้ว')
  const submit = () => run(() => api('POST', `/contents/${c.id}/submit`), 'ส่งขออนุมัติแล้ว')
  const approve = () => run(() => api('POST', `/contents/${c.id}/approve`), 'อนุมัติแล้ว')
  const reject = () => run(async () => { await api('POST', `/contents/${c.id}/reject`, { reason }); setRejecting(false); setReason('') }, 'ปฏิเสธแล้ว')

  async function copyText() {
    try { await navigator.clipboard.writeText(c.body) } catch {
      const t = document.createElement('textarea'); t.value = c.body; document.body.appendChild(t); t.select(); document.execCommand('copy'); t.remove()
    }
    setCopied(true); setTimeout(() => setCopied(false), 2000)
  }
  async function downloadImage() {
    try {
      const blob = await (await fetch(image)).blob()
      const a = document.createElement('a')
      a.href = URL.createObjectURL(blob)
      a.download = `leadloop-ad-${c.id}.${(blob.type.split('/')[1] || 'jpg').replace('jpeg', 'jpg')}`
      document.body.appendChild(a); a.click(); a.remove()
      setTimeout(() => URL.revokeObjectURL(a.href), 1000)
    } catch { onError('ดาวน์โหลดรูปไม่ได้ (ถ้าเพิ่งตั้งค่า ให้เปิด PUBLIC_CHAT_ENABLED=1 แล้วลองใหม่)') }
  }

  const editable = mayEdit && (c.status === 'draft' || c.status === 'rejected' || c.status === 'pending_approval' || c.status === 'approved' || c.status === 'published')
  return (
    <li className="card">
      <div className="card-top">
        {showCampaign && <span className="card-camp">{c.campaign_name}</span>}
        <span className="tag">{CHANNELS[c.channel] || c.channel}</span>
        <span className={`tag tag-c-${c.status}`}>{CONTENT_STATUS[c.status] || c.status}</span>
        <span className={`src src-${c.generated_by}`}>{SOURCE[c.generated_by] || c.generated_by}</span>
      </div>

      <div className="ad">
        {image && <img className="ad-img" src={image} alt="รูปสินค้าในโฆษณา" loading="lazy" onError={(e) => { e.currentTarget.style.display = 'none' }} />}
        {editing ? (
          <div className="ad-edit">
            <textarea rows="6" maxLength={5000} value={text} onChange={(e) => setText(e.target.value)} aria-label="ข้อความคอนเทนต์" />
            {(c.status === 'approved' || c.status === 'published') && <p className="small own-note">คอนเทนต์นี้อนุมัติแล้ว ถ้าแก้ จะกลับไปสถานะ "รออนุมัติ" และต้องให้ผู้อนุมัติดูใหม่</p>}
            <div className="card-actions">
              <button className="btn btn-primary" disabled={busy || !text.trim()} onClick={save}>บันทึก</button>
              <button className="btn btn-quiet" disabled={busy} onClick={() => { setEditing(false); setText(c.body) }}>ยกเลิก</button>
            </div>
          </div>
        ) : <p className="card-body ad-text">{c.body}</p>}
      </div>

      {c.status === 'rejected' && c.reject_reason && (
        <div className="alert alert-error reject-box" role="note">
          <strong>เหตุผลที่ถูกปฏิเสธ</strong>
          <span>{c.reject_reason}</span>
          <span className="small muted">โดย {personLabel(c.rejected_by_name, c.rejected_by_title, c.rejected_by)}{c.rejected_at ? ` · ${timeAgo(c.rejected_at)}` : ''}</span>
        </div>
      )}

      <div className="card-meta muted small">
        สร้างโดย {personLabel(c.created_by_name, c.created_by_title, c.created_by)} · {timeAgo(c.created_at)}
        {c.status === 'approved' && c.approved_by && <> · อนุมัติโดย {personLabel(c.approved_by_name, c.approved_by_title, c.approved_by)}{c.approved_at ? ` (${fmtDateTime(c.approved_at)})` : ''}</>}
        {c.edited_at && <> · แก้ไขล่าสุด {timeAgo(c.edited_at)}</>}
      </div>

      <div className="card-actions">
        <button className="btn btn-quiet" onClick={copyText}>{copied ? 'คัดลอกแล้ว' : 'คัดลอกข้อความ'}</button>
        {image && <button className="btn btn-quiet" onClick={downloadImage}>ดาวน์โหลดรูป</button>}
        {editable && !editing && <button className="btn btn-quiet" disabled={busy} onClick={() => { setText(c.body); setEditing(true) }}>แก้ข้อความ</button>}
        {mayEdit && (c.status === 'draft' || c.status === 'rejected') && !editing && (
          <button className="btn btn-primary" disabled={busy} onClick={submit}>{c.status === 'rejected' ? 'ส่งขออนุมัติอีกครั้ง' : 'ส่งขออนุมัติ'}</button>
        )}
        {c.status === 'pending_approval' && mayDecide && (
          <>
            <button className="btn btn-primary" disabled={busy} onClick={approve}>อนุมัติ</button>
            <button className="btn btn-quiet btn-danger" disabled={busy} onClick={() => setRejecting(true)}>ปฏิเสธ</button>
          </>
        )}
      </div>
      {c.status === 'pending_approval' && !mayDecide && user.role !== 'sales' && (
        <p className="muted small">⏳ รอเพื่อนร่วมทีมอนุมัติ (ผู้สร้างอนุมัติเองไม่ได้) · <Link to="/approvals" className="link">ไปหน้าอนุมัติ</Link></p>
      )}
      {c.status === 'pending_approval' && user.role === 'sales' && <p className="muted small">รอผู้อนุมัติ (เซลส์ดูได้อย่างเดียว)</p>}

      {rejecting && (
        <Modal title="ปฏิเสธคอนเทนต์นี้" onClose={() => setRejecting(false)}>
          <p className="card-body quote">{c.body}</p>
          <label className="field"><span>เหตุผลหรือข้อเสนอแนะ (จำเป็น ผู้สร้างจะเห็นข้อความนี้)</span>
            <textarea rows="4" maxLength={1000} value={reason} onChange={(e) => setReason(e.target.value)} placeholder="เช่น ราคาไม่ตรงกับโปรจริง ช่วยแก้ประโยคที่สองและเพิ่มเงื่อนไขการรับประกัน" autoFocus />
          </label>
          <div className="modal-foot">
            <button className="btn btn-quiet" onClick={() => setRejecting(false)}>ยกเลิก</button>
            <button className="btn btn-danger-solid" disabled={busy || !reason.trim()} onClick={reject}>ปฏิเสธ</button>
          </div>
        </Modal>
      )}
    </li>
  )
}
