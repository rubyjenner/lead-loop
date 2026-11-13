import { useCallback, useEffect, useState } from 'react'
import { api } from '../api.js'
import { Modal } from '../ui.jsx'

function Status({ on, yes = 'เชื่อมต่อแล้ว', no = 'ยังไม่เชื่อมต่อ' }) {
  return <span className={`integration-status ${on ? 'is-on' : 'is-off'}`}><i />{on ? yes : no}</span>
}

export default function Integrations() {
  const [data, setData] = useState(null)
  const [error, setError] = useState('')
  const [showConnect, setShowConnect] = useState(false)
  const [busy, setBusy] = useState(false)
  const [form, setForm] = useState({ account_name: '', channel_secret: '', access_token: '' })
  const load = useCallback(() => api('GET', '/integrations/status').then(setData).catch((e) => setError(e.message)), [])
  useEffect(() => { load() }, [load])
  const line = data?.line || {}

  async function connect(e) {
    e.preventDefault(); setBusy(true); setError('')
    try {
      await api('POST', '/integrations/line/connect', form)
      setForm({ account_name: '', channel_secret: '', access_token: '' })
      setShowConnect(false)
      await load()
    } catch (e) { setError(e.message) } finally { setBusy(false) }
  }
  async function disconnect() {
    if (!window.confirm('ต้องการยกเลิกการเชื่อมต่อ LINE Official Account หรือไม่?')) return
    setBusy(true); setError('')
    try { await api('DELETE', '/integrations/line'); await load() }
    catch (e) { setError(e.message) } finally { setBusy(false) }
  }

  return (
    <div className="page narrow">
      <header className="page-head"><div><h1>การเชื่อมต่อระบบ</h1><p className="muted">เชื่อมต่อช่องทางของบริษัทกับ LeadLoop โดยเก็บข้อมูลรับรองไว้ในระบบอย่างเข้ารหัส</p></div></header>
      {error && <div className="alert alert-error" role="alert">{error}</div>}
      <section className="card integration-card">
        <div className="integration-title"><div><span className="integration-logo line-logo">LINE</span><div><h2>LINE Official Account</h2><p className="muted small">รับข้อความจากลูกค้า สร้างลีด และให้ทีมขายตอบกลับจากหน้า LeadLoop</p></div></div><Status on={!!line.verified} yes="เชื่อมต่อและตรวจสอบแล้ว" no={line.enabled ? 'ตั้งค่าแล้ว · ยังไม่ยืนยัน' : 'ยังไม่เชื่อมต่อ'} /></div>
        <div className="integration-checks">
          <div><span>บัญชีบริษัท</span><strong>{line.account_name || 'ยังไม่ได้ระบุ'}</strong></div>
          <div><span>LINE Bot</span><strong>{line.bot_name || 'ยังไม่ได้ตรวจสอบ'}</strong></div>
          <div><span>Channel Secret</span><Status on={!!line.secret_configured} yes="จัดเก็บแล้ว" no="ยังไม่มี" /></div>
          <div><span>Channel Access Token</span><Status on={!!line.token_configured} yes="จัดเก็บแล้ว" no="ยังไม่มี" /></div>
          <div><span>Webhook URL path</span><code>{line.webhook_path || '/api/public/line/webhook'}</code></div>
          {line.verified_at && <div><span>ตรวจสอบล่าสุด</span><strong>{new Date(line.verified_at).toLocaleString('th-TH')}</strong></div>}
        </div>
        {!data?.encryption_ready && <div className="alert alert-warn">เซิร์ฟเวอร์ยังไม่ได้ตั้งค่า INTEGRATION_ENCRYPTION_KEY (ต้องเป็นข้อความสุ่ม 32 ตัวอักษร) จึงยังบันทึกข้อมูลเชื่อมต่ออย่างปลอดภัยไม่ได้</div>}
        <div className="actions">
          <button className="btn btn-primary" disabled={!data?.encryption_ready || busy} onClick={() => setShowConnect(true)}>{line.enabled ? 'เปลี่ยนบัญชี / อัปเดตข้อมูล' : 'เชื่อมต่อ LINE Official Account'}</button>
          {line.enabled && <button className="btn btn-quiet btn-danger" disabled={busy} onClick={disconnect}>ยกเลิกการเชื่อมต่อ</button>}
        </div>
        <details className="integration-steps"><summary>วิธีเตรียมข้อมูล LINE</summary><ol><li>เปิด LINE Official Account Manager ของบริษัท</li><li>เปิด Messaging API และเลือก Provider / Channel ที่บริษัทเป็นเจ้าของ</li><li>คัดลอก Channel Secret และออก Channel Access Token จาก LINE Developers</li><li>นำมากรอกในหน้าต่างเชื่อมต่อนี้ ระบบจะตรวจสอบ Bot Profile ก่อนบันทึก</li><li>นำ Webhook URL ของระบบไปตั้งใน LINE Developers แล้วกด Verify และเปิด Use webhook</li></ol><p className="small muted">LINE Messaging API ไม่มีปุ่ม OAuth กลางที่ให้ LeadLoop ขอสิทธิ์จัดการทุก Official Account ได้เหมือนการ Sign in ทั่วไป ขั้นตอนนี้จึงให้ผู้ดูแลบัญชีบริษัทนำ credentials ของบัญชีที่ตนมีสิทธิ์จัดการมาเชื่อมต่อผ่าน HTTPS</p></details>
      </section>
      <section className="card integration-card">
        <div className="integration-title"><div><span className="integration-logo web-logo">WEB</span><div><h2>หน้าแชตบนเว็บไซต์</h2><p className="muted small">ให้ลูกค้าคุยผ่านหน้าเว็บสาธารณะของ LeadLoop</p></div></div><Status on={!!data?.web_chat?.enabled} yes="เปิดใช้งานแล้ว" no="ปิดอยู่" /></div>
        <p className="small muted">เปิดใช้งานด้วย PUBLIC_CHAT_ENABLED=1 เมื่อพร้อมให้ลูกค้าภายนอกทดลองใช้ และควรตั้งโดเมน HTTPS กับนโยบายความเป็นส่วนตัวให้เรียบร้อย</p>
      </section>
      <section className="card integration-card">
        <h2>ช่องทางที่เตรียมรองรับในอนาคต</h2>
        <p className="muted small">โครงสร้างระบบแยกช่องทางรับข้อความออกจากการจัดการลีด เพื่อให้เพิ่มช่องทางอื่นได้โดยไม่ต้องเขียนระบบคะแนนและการติดตามใหม่ทั้งหมด</p>
        <div className="integration-roadmap"><span>Facebook Messenger</span><span>Instagram</span><span>เว็บไซต์ / Webhook</span><span>CRM ภายนอก</span></div>
        <p className="small muted">รายการเหล่านี้ยังไม่ได้เชื่อมต่อจริง ต้องพัฒนาและกำหนดสิทธิ์/ข้อมูลที่ส่งต่อก่อนเปิดใช้</p>
      </section>
      {showConnect && <Modal title="เชื่อมต่อ LINE Official Account" onClose={() => setShowConnect(false)}>
        <form className="form" onSubmit={connect}>
          <p className="muted small">ข้อมูลจะถูกส่งผ่าน API และเข้ารหัสก่อนจัดเก็บในฐานข้อมูลของ LeadLoop จะไม่แสดง Token กลับมาในหน้าเว็บ</p>
          <label className="field"><span>ชื่อบัญชี / บริษัท</span><input value={form.account_name} onChange={(e) => setForm({ ...form, account_name: e.target.value })} required placeholder="เช่น บริษัท ABC จำกัด" /></label>
          <label className="field"><span>Channel Secret</span><input type="password" autoComplete="new-password" value={form.channel_secret} onChange={(e) => setForm({ ...form, channel_secret: e.target.value })} required /></label>
          <label className="field"><span>Channel Access Token</span><textarea rows="3" autoComplete="new-password" value={form.access_token} onChange={(e) => setForm({ ...form, access_token: e.target.value })} required /></label>
          {error && <div className="alert alert-error">{error}</div>}
          <div className="modal-foot"><button type="button" className="btn btn-quiet" onClick={() => setShowConnect(false)}>ยกเลิก</button><button className="btn btn-primary" disabled={busy || !form.account_name.trim() || !form.channel_secret.trim() || !form.access_token.trim()}>{busy ? 'กำลังตรวจสอบ…' : 'ตรวจสอบและเชื่อมต่อ'}</button></div>
        </form>
      </Modal>}
    </div>
  )
}
