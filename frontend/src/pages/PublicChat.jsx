import { useEffect, useRef, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { api } from '../api.js'

// Public customer chat (no login). A test customer opens /chat?c=<campaign id>, accepts the consent notice and
// talks to the system. The team sees the lead appear in the Leads page. Test names only, never real data.
const QUICK = ['สนใจค่ะ ขอรายละเอียดหน่อย', 'ราคาเท่าไหร่คะ', 'ผ่อนได้ไหมคะ', 'ขอคุยกับเจ้าหน้าที่ค่ะ', 'ช่วยโทรกลับด้วยนะคะ']
const SID_KEY = 'leadloop.chat.sid'

function newSid() {
  const a = new Uint8Array(16)
  try { crypto.getRandomValues(a) } catch { for (let i = 0; i < 16; i++) a[i] = Math.floor(Math.random() * 256) }
  return Array.from(a, (b) => b.toString(16).padStart(2, '0')).join('')
}
function loadSid() {
  try { let s = sessionStorage.getItem(SID_KEY); if (!s) { s = newSid(); sessionStorage.setItem(SID_KEY, s) } return s } catch { return newSid() }
}

export default function PublicChat() {
  const [params] = useSearchParams()
  const campaignId = Number(params.get('c')) || null
  const [sid, setSid] = useState(loadSid)
  const [info, setInfo] = useState(null)
  const [log, setLog] = useState([{ from: 'bot', text: 'สวัสดีค่ะ ยินดีต้อนรับ พิมพ์ข้อความหรือกดปุ่มด้านล่างเพื่อเริ่มคุยได้เลยค่ะ' }])
  const [text, setText] = useState('')
  const [pending, setPending] = useState(null)
  const [name, setName] = useState('')
  const [marketing, setMarketing] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [ended, setEnded] = useState(false)
  const end = useRef(null)

  useEffect(() => {
    api('GET', campaignId ? `/public/shop?campaign_id=${campaignId}` : '/public/chat/info').then(setInfo).catch(() => setInfo({ enabled: false }))
  }, [campaignId])
  useEffect(() => { end.current?.scrollIntoView({ block: 'end' }) }, [log, pending])

  const push = (m) => setLog((l) => [...l, m])

  async function send(t) {
    const msg = (t ?? text).trim()
    if (!msg || busy || ended) return
    setText(''); setError(''); setBusy(true)
    push({ from: 'cust', text: msg })
    try {
      const r = await api('POST', '/public/chat/messages', { session_id: sid, text: msg })
      push({ from: 'bot', text: r.reply })
      setPending(r.needs_consent ? msg : null)
    } catch (e) { setError(e.message) } finally { setBusy(false) }
  }

  async function accept() {
    setBusy(true); setError('')
    try {
      const body = { session_id: sid, display_name: name.trim(), consent_marketing: marketing }
      if (campaignId) body.campaign_id = campaignId
      const r = await api('POST', '/public/chat/consent', body)
      push({ from: 'cust', text: marketing ? 'ยินยอม (รวมรับข่าวสารและโปรโมชัน)' : 'ยินยอม' })
      push({ from: 'bot', text: r.reply })
      const again = pending; setPending(null); setBusy(false)
      if (again) await send(again)
    } catch (e) { setError(e.message); setBusy(false) }
  }

  function decline() {
    push({ from: 'cust', text: 'ไม่ยินยอม' })
    push({ from: 'bot', text: 'รับทราบค่ะ ระบบไม่ได้เก็บข้อมูลใดๆ ของคุณ หากเปลี่ยนใจ ส่งข้อความมาใหม่ได้ตลอดค่ะ' })
    setPending(null)
  }

  async function withdraw() {
    if (busy) return
    setBusy(true); setError('')
    try {
      const r = await api('POST', '/public/chat/withdraw', { session_id: sid })
      push({ from: 'bot', text: r.reply }); setPending(null)
    } catch (e) { setError(e.message) } finally { setBusy(false) }
  }

  function restart() {
    const s = newSid()
    try { sessionStorage.setItem(SID_KEY, s) } catch { /* private window */ }
    setSid(s); setPending(null); setMarketing(false); setEnded(false); setError('')
    setLog([{ from: 'bot', text: 'เริ่มบทสนทนาใหม่ในฐานะลูกค้าคนใหม่ค่ะ' }])
  }

  if (!info) return <div className="pub"><p className="muted">กำลังโหลด…</p></div>
  if (!info.enabled) return <div className="pub"><div className="pub-card"><h1>แชตนี้ปิดอยู่</h1><p className="muted">ขณะนี้ยังไม่เปิดให้ทดลองใช้งาน</p></div></div>

  return (
    <div className="pub">
      <div className="pub-card">
        <header className="pub-head">
          <h1>{info.campaign_name || 'แชตกับร้าน'}</h1>
          {info.product_name && <p className="muted">{info.product_name}{info.product_price != null ? ` · ${Number(info.product_price).toLocaleString('th-TH')} บาท` : ''}</p>}
        </header>
        {info.image_url && <img className="shop-img" src={info.image_url} alt={info.product_name || 'สินค้า'} loading="lazy" referrerPolicy="no-referrer" />}
        {info.description && <p className="shop-desc">{info.description}</p>}
        {(info.posts || []).map((t, i) => <div key={i} className="shop-post">{t}</div>)}
        <div className="alert alert-info small">ระบบทดลองของทีมพัฒนา ใช้ชื่อสมมติเท่านั้น ห้ามกรอกข้อมูลส่วนตัวจริง</div>
        <div className="chat-log" aria-live="polite">
          {log.map((m, i) => <div key={i} className={`bubble bubble-${m.from}`}>{m.text}</div>)}
          {pending && (
            <div className="consent-card">
              <b>ขอความยินยอมก่อนคุยต่อ</b>
              <label className="field"><span>ชื่อที่ให้เรียก (ไม่บังคับ ใช้ชื่อสมมติได้)</span>
                <input value={name} maxLength={60} onChange={(e) => setName(e.target.value)} placeholder="เช่น คุณทดสอบ" /></label>
              <label className="checks"><input type="checkbox" checked={marketing} onChange={(e) => setMarketing(e.target.checked)} /> ยินยอมรับข่าวสารและโปรโมชันจากร้าน (ไม่บังคับ)</label>
              <div className="actions">
                <button className="btn btn-primary" onClick={accept} disabled={busy}>ยินยอมและเริ่มคุย</button>
                <button className="btn btn-quiet" onClick={decline} disabled={busy}>ไม่ยินยอม</button>
              </div>
            </div>
          )}
          <div ref={end} />
        </div>
        {error && <div className="alert alert-error" role="alert">{error}</div>}
        <div className="chips">{QUICK.map((q) => <button key={q} className="chip" onClick={() => send(q)} disabled={busy || !!pending}>{q}</button>)}</div>
        <form className="chat-input" onSubmit={(e) => { e.preventDefault(); send() }}>
          <input value={text} maxLength={500} onChange={(e) => setText(e.target.value)} placeholder="พิมพ์ข้อความ" disabled={!!pending} aria-label="ข้อความ" />
          <button className="btn btn-primary" disabled={busy || !!pending || !text.trim()}>ส่ง</button>
        </form>
        <footer className="pub-foot">
          <button className="link-btn" onClick={withdraw} disabled={busy}>ถอนความยินยอมและหยุดคุย</button>
          <button className="link-btn" onClick={restart}>เริ่มใหม่ (ลูกค้าคนใหม่)</button>
        </footer>
      </div>
    </div>
  )
}
