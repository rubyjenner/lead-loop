import { useCallback, useEffect, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { api } from '../api.js'
import { useAuth } from '../auth.jsx'
import { CAMPAIGN_STATUS, Modal, Toast, baht, timeAgo } from '../ui.jsx'

export default function Campaigns() {
  const { user } = useAuth()
  const [list, setList] = useState(null)
  const [products, setProducts] = useState([])
  const [editing, setEditing] = useState(null) // product whose facts are being edited
  const [error, setError] = useState('')
  const [modal, setModal] = useState(null) // 'campaign' | 'product'
  const [toast, setToast] = useState('')
  const canEdit = user.role === 'admin' || user.role === 'marketing'

  const load = useCallback(async () => {
    try {
      const [c, p, facts] = await Promise.all([api('GET', '/campaigns'), api('GET', '/products'), api('GET', '/product-facts').catch(() => [])])
      setList(c); setProducts(p.map((x) => ({ ...x, ...(facts.find((f) => f.id === x.id) || {}) }))); setError('')
    } catch (e) { setError(e.message) }
  }, [])
  useEffect(() => { load() }, [load])

  const product = (id) => products.find((p) => p.id === id)

  return (
    <div className="page">
      <header className="page-head">
        <div>
          <h1>แคมเปญ</h1>
          <p className="muted">เริ่มจากเลือกสินค้า ตั้งเป้าหมายและงบ แล้วให้ AI วางแผนและร่างคอนเทนต์ คอนเทนต์ต้องผ่านการอนุมัติก่อนใช้</p>
        </div>
        {canEdit && <button className="btn btn-primary" onClick={() => setModal('campaign')}>สร้างแคมเปญ</button>}
      </header>
      {error && <div className="alert alert-error" role="alert">{error}</div>}
      {!list && !error && <div className="muted pad">กำลังโหลด…</div>}
      {list && list.length === 0 && (
        <div className="empty">
          <p className="empty-title">ยังไม่มีแคมเปญ</p>
          <p className="muted">{canEdit ? 'กด "สร้างแคมเปญ" เพื่อเริ่ม ถ้ายังไม่มีสินค้า ให้เพิ่มสินค้าในหน้าต่างสร้างแคมเปญก่อน' : 'เมื่อทีมการตลาดสร้างแคมเปญ จะแสดงที่นี่'}</p>
        </div>
      )}
      {list && list.length > 0 && (
        <div className="table-wrap">
          <table className="table">
            <thead><tr><th>แคมเปญ</th><th>สินค้า</th><th>เป้าหมาย</th><th className="right">งบ</th><th>สถานะ</th><th>สร้างเมื่อ</th></tr></thead>
            <tbody>
              {list.map((c) => (
                <tr key={c.id}>
                  <td><Link className="link" to={`/campaigns/${c.id}`}>{c.name}</Link></td>
                  <td>{product(c.product_id)?.name || <span className="muted">ไม่ได้ผูกสินค้า</span>}</td>
                  <td className="clip">{c.goal || <span className="muted">-</span>}</td>
                  <td className="right nowrap">{baht(c.budget)}</td>
                  <td><span className={`tag tag-${c.status}`}>{CAMPAIGN_STATUS[c.status] || c.status}</span></td>
                  <td className="muted nowrap">{timeAgo(c.created_at)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {products.length > 0 && (
        <section className="panel" style={{ marginTop: 18 }}>
          <h2>สินค้าและข้อมูลที่บอตตอบลูกค้าได้</h2>
          <p className="muted small">บอตตอบลูกค้าจากข้อมูลที่กรอกไว้เท่านั้น ช่องไหนว่าง บอตจะส่งคำถามนั้นให้เจ้าหน้าที่ตอบ ไม่เดาเอง</p>
          <div className="table-wrap">
            <table className="table">
              <thead><tr><th>สินค้า</th><th className="right">ราคา</th><th>ข้อมูลครบ</th><th></th></tr></thead>
              <tbody>
                {products.map((p) => {
                  const n = factCount(p)
                  return (
                    <tr key={p.id}>
                      <td>{p.name}</td>
                      <td className="right nowrap">{p.price != null ? baht(p.price) : <span className="muted">-</span>}</td>
                      <td><span className={`tag ${n === FACT_TOTAL ? 'tag-l-closed' : 'tag-l-new'}`}>{n}/{FACT_TOTAL}</span></td>
                      <td className="right">{canEdit && <button className="btn btn-quiet" onClick={() => setEditing(p)}>แก้ไขสินค้า</button>}</td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        </section>
      )}
      {editing && <FactsModal product={editing} onClose={() => setEditing(null)} onSaved={async () => { setEditing(null); await load(); setToast('บันทึกข้อมูลสินค้าแล้ว') }} />}
      {modal === 'campaign' && (
        <CampaignModal products={products} onClose={() => setModal(null)} onAddProduct={() => setModal('product')}
          onCreated={() => { setModal(null); load() }} />
      )}
      {modal === 'product' && (
        <ProductModal onClose={() => setModal('campaign')} onCreated={async () => { await load(); setToast('เพิ่มสินค้าแล้ว'); setModal('campaign') }} />
      )}
      <Toast msg={toast} onDone={() => setToast('')} />
    </div>
  )
}

function CampaignModal({ products, onClose, onCreated, onAddProduct }) {
  const nav = useNavigate()
  const [f, setF] = useState({ name: '', product_id: products[0]?.id || '', goal: '', target_audience: '', budget: '' })
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const set = (k) => (e) => setF({ ...f, [k]: e.target.value })

  async function submit(e) {
    e.preventDefault()
    setError(''); setBusy(true)
    try {
      const body = { name: f.name.trim(), goal: f.goal.trim(), target_audience: f.target_audience.trim(), budget: Number(f.budget) || 0 }
      if (f.product_id) body.product_id = Number(f.product_id)
      const c = await api('POST', '/campaigns', body)
      onCreated()
      nav(`/campaigns/${c.id}`)
    } catch (err) { setError(err.message) } finally { setBusy(false) }
  }
  return (
    <Modal title="สร้างแคมเปญ" onClose={onClose}>
      <form className="form" onSubmit={submit} noValidate>
        {error && <div className="alert alert-error" role="alert">{error}</div>}
        <label className="field"><span>ชื่อแคมเปญ</span><input value={f.name} onChange={set('name')} placeholder="เช่น ลดล้างสต็อกมือถือมือสอง" autoFocus /></label>
        <label className="field"><span>สินค้า</span>
          <select value={f.product_id} onChange={set('product_id')}>
            <option value="">ไม่ผูกสินค้า</option>
            {products.map((p) => <option key={p.id} value={p.id}>{p.name}{p.price != null ? ` (${baht(p.price)})` : ''}</option>)}
          </select>
          <small>AI จะเขียนจากข้อมูลสินค้านี้เท่านั้น ไม่แต่งข้อมูลเพิ่ม <button type="button" className="linkbtn" onClick={onAddProduct}>เพิ่มสินค้าใหม่</button></small>
        </label>
        <label className="field"><span>เป้าหมาย</span><input value={f.goal} onChange={set('goal')} placeholder="เช่น ได้ลีด 50 คนภายในเดือนนี้" /></label>
        <label className="field"><span>กลุ่มเป้าหมาย</span><input value={f.target_audience} onChange={set('target_audience')} placeholder="เช่น นักศึกษาและคนทำงานรุ่นใหม่" /></label>
        <label className="field"><span>งบประมาณ (บาท)</span><input type="number" min="0" value={f.budget} onChange={set('budget')} /></label>
        <div className="modal-foot">
          <button type="button" className="btn btn-quiet" onClick={onClose}>ยกเลิก</button>
          <button className="btn btn-primary" disabled={busy || !f.name.trim()}>{busy ? 'กำลังสร้าง…' : 'สร้างแคมเปญ'}</button>
        </div>
      </form>
    </Modal>
  )
}

const FACT_FIELDS = [
  ['variants', 'รุ่น / ขนาด / สี', 'เช่น ไซส์ S M L, สีขาว ดำ'],
  ['product_type', 'แบบสินค้า', 'เช่น คอกลม แขนสั้น'],
  ['after_sales', 'บริการหลังการขาย', 'เช่น รับประกัน 1 ปี เปลี่ยนไซส์ได้ใน 7 วัน'],
  ['payment_methods', 'ชำระเงินได้ด้วยอะไรบ้าง', 'เช่น โอนผ่านพร้อมเพย์ บัตรเครดิต เก็บเงินปลายทาง'],
  ['installment_info', 'ผ่อนได้ไหม', 'เช่น ผ่อน 0% 3 เดือนผ่านบัตรเครดิต หรือ ไม่รับผ่อน'],
]
const FACT_TOTAL = FACT_FIELDS.length + 1 // + price
const factCount = (p) => FACT_FIELDS.filter(([k]) => (p[k] || '').trim()).length + (p.price != null && p.price !== '' ? 1 : 0)

function FactsFields({ f, set }) {
  return FACT_FIELDS.map(([k, label, ph]) => (
    <label className="field" key={k}><span>{label}</span>
      <textarea rows="2" maxLength={500} value={f[k] || ''} onChange={set(k)} placeholder={ph} /></label>
  ))
}

function FactsModal({ product, onClose, onSaved }) {
  const [f, setF] = useState(() => ({
    ...Object.fromEntries(FACT_FIELDS.map(([k]) => [k, product[k] || ''])),
    name: product.name || '', description: product.description || '', price: product.price ?? '', brand_info: product.brand_info || '',
  }))
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const set = (k) => (e) => setF({ ...f, [k]: e.target.value })
  async function submit(e) {
    e.preventDefault()
    setError(''); setBusy(true)
    try {
      await api('PUT', `/products/${product.id}`, { name: f.name.trim(), description: f.description.trim(), price: f.price === '' ? null : Number(f.price), brand_info: f.brand_info.trim() })
      await api('PUT', `/product-facts/${product.id}`, Object.fromEntries(FACT_FIELDS.map(([k]) => [k, f[k]])))
      onSaved()
    } catch (err) { setError(err.message) } finally { setBusy(false) }
  }
  return (
    <Modal title={`แก้ไขสินค้า: ${product.name}`} onClose={onClose}>
      <form className="form" onSubmit={submit} noValidate>
        {error && <div className="alert alert-error" role="alert">{error}</div>}
        <label className="field"><span>ชื่อสินค้า</span><input value={f.name} onChange={set('name')} /></label>
        <label className="field"><span>รายละเอียด</span><textarea rows="3" value={f.description} onChange={set('description')} /></label>
        <label className="field"><span>ราคา (บาท)</span><input type="number" min="0" value={f.price} onChange={set('price')} /></label>
        <p className="muted small">ใส่เฉพาะข้อมูลจริง บอตจะตอบลูกค้าตามข้อความด้านล่างตรง ๆ ช่องที่เว้นว่างบอตจะส่งต่อให้เจ้าหน้าที่ตอบ</p>
        <FactsFields f={f} set={set} />
        <div className="modal-foot">
          <button type="button" className="btn btn-quiet" onClick={onClose}>ยกเลิก</button>
          <button className="btn btn-primary" disabled={busy || !f.name.trim()}>{busy ? 'กำลังบันทึก…' : 'บันทึก'}</button>
        </div>
      </form>
    </Modal>
  )
}

function readAsDataURL(file) {
  return new Promise((resolve, reject) => {
    const r = new FileReader()
    r.onload = () => resolve(String(r.result))
    r.onerror = () => reject(new Error('อ่านไฟล์ไม่ได้'))
    r.readAsDataURL(file)
  })
}

function ProductModal({ onClose, onCreated }) {
  const [f, setF] = useState({ name: '', description: '', price: '', brand_info: '', image_url: '', variants: '', product_type: '', after_sales: '', payment_methods: '', installment_info: '' })
  const [file, setFile] = useState(null)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const set = (k) => (e) => setF({ ...f, [k]: e.target.value })
  async function submit(e) {
    e.preventDefault()
    setError(''); setBusy(true)
    try {
      const body = { name: f.name.trim(), description: f.description.trim(), brand_info: f.brand_info.trim() }
      if (f.price !== '') body.price = Number(f.price)
      if (f.image_url.trim() && !file) body.image_url = f.image_url.trim()
      const created = await api('POST', '/products', body)
      if (file && created?.id) {
        try { await api('POST', `/products/${created.id}/image`, { data_url: await readAsDataURL(file) }) }
        catch (err2) { setError(`เพิ่มสินค้าแล้ว แต่อัปโหลดรูปไม่สำเร็จ: ${err2.message}`); return }
      }
      if (created?.id && FACT_FIELDS.some(([k]) => f[k].trim())) {
        try { await api('PUT', `/product-facts/${created.id}`, Object.fromEntries(FACT_FIELDS.map(([k]) => [k, f[k]]))) }
        catch (err3) { setError(`เพิ่มสินค้าแล้ว แต่บันทึกข้อมูลสินค้าไม่สำเร็จ: ${err3.message} (แก้ภายหลังได้ที่ปุ่ม "แก้ไขสินค้า")`); return }
      }
      onCreated()
    } catch (err) { setError(err.message) } finally { setBusy(false) }
  }
  return (
    <Modal title="เพิ่มสินค้า" onClose={onClose}>
      <form className="form" onSubmit={submit} noValidate>
        {error && <div className="alert alert-error" role="alert">{error}</div>}
        <label className="field"><span>ชื่อสินค้า</span><input value={f.name} onChange={set('name')} autoFocus /></label>
        <label className="field"><span>รายละเอียด</span><textarea rows="3" value={f.description} onChange={set('description')} />
          <small>ใส่เฉพาะข้อมูลจริง เช่น สภาพ ขนาด การรับประกัน คำตอบของแชตและคอนเทนต์ที่ AI เขียนจะอ้างจากข้อมูลนี้</small></label>
        <label className="field"><span>ราคา (บาท)</span><input type="number" min="0" value={f.price} onChange={set('price')} /></label>
        <label className="field"><span>อัปโหลดรูปสินค้า (png, jpeg, webp ไม่เกิน 2 MB)</span>
          <input type="file" accept="image/png,image/jpeg,image/webp" onChange={(e) => {
            const x = e.target.files?.[0] || null
            if (x && x.size > 2 * 1024 * 1024) { setError('รูปใหญ่เกิน 2 MB'); e.target.value = ''; setFile(null); return }
            setError(''); setFile(x)
          }} />
          <small>ใช้รูปที่เป็นของร้านเอง ห้ามมีรูปหน้าหรือข้อมูลส่วนตัวของลูกค้า รูปจะแสดงบนหน้าร้านสาธารณะ และไม่ถูกส่งให้ AI</small></label>
        <label className="field"><span>หรือใส่ลิงก์รูปสินค้า (ถ้ามี)</span><input type="url" placeholder="https://…" value={f.image_url} onChange={set('image_url')} />
          <small>ใช้ลิงก์รูปที่เป็นของร้านเอง รูปจะแสดงบนหน้าร้านสาธารณะ</small></label>
        <h3 style={{ margin: '14px 0 2px' }}>ข้อมูลที่บอตใช้ตอบลูกค้า (ไม่บังคับ)</h3>
        <FactsFields f={f} set={set} />
        <label className="field"><span>ข้อมูลแบรนด์ (ถ้ามี)</span><input value={f.brand_info} onChange={set('brand_info')} /></label>
        <div className="modal-foot">
          <button type="button" className="btn btn-quiet" onClick={onClose}>ย้อนกลับ</button>
          <button className="btn btn-primary" disabled={busy || !f.name.trim()}>{busy ? 'กำลังบันทึก…' : 'เพิ่มสินค้า'}</button>
        </div>
      </form>
    </Modal>
  )
}
