import { useEffect, useRef, useState } from 'react'

// The loop: five stages of LeadLoop around a ring. Used as the brand mark (small) and on the login page (large).
export const STAGES = ['วางแผน', 'ร่างคอนเทนต์', 'อนุมัติ', 'ลีดและคะแนน', 'ส่งต่อเซลส์']

export function LoopMark({ size = 28 }) {
  return (
    <svg width={size} height={size} viewBox="0 0 32 32" aria-hidden="true">
      <path d="M16 4.5a11.5 11.5 0 1 1-9.6 5.1" fill="none" stroke="currentColor" strokeWidth="3" strokeLinecap="round" />
      <path d="M2.8 6.6l4.2 4.4 4.3-4.9z" fill="currentColor" />
    </svg>
  )
}

export function LoopDiagram() {
  const R = 118, cx = 240, cy = 160
  const pts = STAGES.map((_, i) => {
    const a = (-90 + i * 72) * (Math.PI / 180)
    return [cx + R * Math.cos(a), cy + R * Math.sin(a)]
  })
  return (
    <svg viewBox="0 0 480 330" className="loop-diagram" role="img" aria-label={`วงจร LeadLoop: ${STAGES.join(' → ')} แล้วย้อนกลับไปวางแผน`}>
      <circle cx={cx} cy={cy} r={R} fill="none" stroke="currentColor" strokeOpacity=".28" strokeWidth="1.5" strokeDasharray="2 7" strokeLinecap="round" />
      {pts.map(([x, y], i) => {
        const left = x < cx - 5, right = x > cx + 5
        const anchor = left ? 'end' : right ? 'start' : 'middle'
        const dx = left ? -16 : right ? 16 : 0
        const dy = !left && !right ? (y < cy ? -18 : 28) : 5
        return (
          <g key={i}>
            <circle cx={x} cy={y} r="8" className={i === 0 ? 'node node-start' : 'node'} />
            <text x={x + dx} y={y + dy} textAnchor={anchor} className="node-label">{STAGES[i]}</text>
          </g>
        )
      })}
      <text x={cx} y={cy - 4} textAnchor="middle" className="loop-center">ผลลัพธ์ย้อนกลับ</text>
      <text x={cx} y={cy + 16} textAnchor="middle" className="loop-center">ไปวางแผนรอบถัดไป</text>
    </svg>
  )
}

export function Modal({ title, onClose, children, wide }) {
  const ref = useRef(null)
  useEffect(() => {
    const prev = document.activeElement
    const onKey = (e) => e.key === 'Escape' && onClose()
    document.addEventListener('keydown', onKey)
    ref.current?.querySelector('input,select,button')?.focus()
    return () => { document.removeEventListener('keydown', onKey); prev?.focus?.() }
  }, [onClose])
  return (
    <div className="scrim" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div className={`modal${wide ? ' modal-wide' : ''}`} role="dialog" aria-modal="true" aria-label={title} ref={ref}>
        <div className="modal-head">
          <h2>{title}</h2>
          <button className="icon-btn" onClick={onClose} aria-label="ปิด">×</button>
        </div>
        {children}
      </div>
    </div>
  )
}

// The API returns a path (/set-password?token=...) when APP_BASE_URL is not set: make it a full link.
export function absoluteUrl(v) {
  return v && v.startsWith('/') ? `${window.location.origin}${v}` : v
}

export function CopyField({ value: raw }) {
  const value = absoluteUrl(raw)
  const [done, setDone] = useState(false)
  async function copy() {
    try { await navigator.clipboard.writeText(value) } catch {
      const t = document.createElement('textarea'); t.value = value; document.body.appendChild(t); t.select(); document.execCommand('copy'); t.remove()
    }
    setDone(true)
    setTimeout(() => setDone(false), 2000)
  }
  return (
    <div className="copyfield">
      <input readOnly value={value} onFocus={(e) => e.target.select()} aria-label="ลิงก์" />
      <button className="btn btn-primary" onClick={copy}>{done ? 'คัดลอกแล้ว' : 'คัดลอกลิงก์'}</button>
    </div>
  )
}

export function Toast({ msg, onDone }) {
  useEffect(() => { if (!msg) return; const t = setTimeout(onDone, 3500); return () => clearTimeout(t) }, [msg, onDone])
  if (!msg) return null
  return <div className="toast" role="status">{msg}</div>
}

export function timeAgo(iso) {
  if (!iso) return null
  const s = (Date.now() - new Date(iso).getTime()) / 1000
  if (s < 90) return 'เมื่อสักครู่'
  if (s < 3600) return `${Math.round(s / 60)} นาทีที่แล้ว`
  if (s < 86400) return `${Math.round(s / 3600)} ชั่วโมงที่แล้ว`
  if (s < 86400 * 14) return `${Math.round(s / 86400)} วันที่แล้ว`
  return new Date(iso).toLocaleDateString('th-TH', { day: 'numeric', month: 'short', year: '2-digit' })
}

export function fmtDateTime(iso) {
  return new Date(iso).toLocaleString('th-TH', { day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' })
}

export const ROLES = {
  admin: { label: 'แอดมิน', help: 'จัดการผู้ใช้ ดูบันทึกการใช้งาน และทำได้ทุกอย่างที่ทีมการตลาดทำ' },
  marketing: { label: 'การตลาด', help: 'วางแผนแคมเปญ ให้ AI ร่างคอนเทนต์ อนุมัติ และจัดการลีด' },
  sales: { label: 'เซลส์', help: 'ดูข้อมูล รับลีดที่ส่งต่อมา และอัปเดตสถานะการขาย' },
}

export const CHANNELS = { line: 'LINE', facebook: 'Facebook', instagram: 'Instagram', tiktok: 'TikTok', email: 'อีเมล' }
export const SOURCE = {
  claude: 'ร่างโดย AI (Claude)',
  gemini: 'ร่างโดย AI (Gemini)',
  template: 'ร่างจากเทมเพลต (AI ใช้ไม่ได้ตอนนั้น)',
  human: 'เขียนโดยคน',
}
export const CAMPAIGN_STATUS = { draft: 'ร่าง', planned: 'วางแผนแล้ว', running: 'กำลังรัน', finished: 'จบแล้ว' }
export const CONTENT_STATUS = { draft: 'ร่าง', pending_approval: 'รออนุมัติ', approved: 'อนุมัติแล้ว', published: 'เผยแพร่แล้ว', rejected: 'ถูกปฏิเสธ' }
export const baht = (v) => (v == null ? '-' : Number(v).toLocaleString('th-TH', { maximumFractionDigits: 2 }) + ' บาท')

export const LEVEL = { cold: 'Cold', warm: 'Warm', hot: 'Hot', disqualified: 'ตัดสิทธิ์' }
export const LEAD_STATUS = { new: 'ใหม่', nurturing: 'กำลังติดตาม', qualified: 'พร้อมส่งต่อ', handed_off: 'ส่งต่อเซลส์แล้ว', closed: 'ปิดการขาย', lost: 'ไม่สำเร็จ' }
export const LEAD_CHANNELS = { line: 'LINE', mock: 'แชตจำลอง', web: 'เว็บ' }
export const RULE_CATEGORY = { intent: 'ความสนใจ', fit: 'ความเหมาะสม', engagement: 'การตอบสนอง', negative: 'สัญญาณลบ' }
export const EVENTS = {
  asked_details: 'ทักขอรายละเอียด', requested_human: 'ขอคุยกับเจ้าหน้าที่', asked_price: 'สอบถามราคา', requested_callback: 'ให้ติดต่อกลับ',
  asked_condition: 'ถามสภาพ/แบตเตอรี่', asked_installment: 'ถามผ่อน', asked_shipping: 'ถามการจัดส่ง', asked_negotiate: 'ต่อรองราคา',
  requested_visit: 'ขอนัดดูเครื่อง', gave_budget: 'ระบุงบ', replied_fast: 'ตอบกลับเร็ว', returned_after_1d: 'กลับมาคุยหลังหาย 1 วัน',
  no_reply_3d: 'ไม่ตอบ 3 วัน', inactive_7d: 'ไม่มีความเคลื่อนไหว 7 วัน', unsubscribed: 'ขอเลิกรับข่าวสาร',
}
export const eventLabel = (k) => EVENTS[k] || k
export const canManage = (user) => user.role === 'admin' || user.role === 'marketing'
