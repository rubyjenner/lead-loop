import { useCallback, useEffect, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { api } from '../api.js'
import { useAuth } from '../auth.jsx'
import { CAMPAIGN_STATUS, Modal, Toast, baht, timeAgo } from '../ui.jsx'

export default function Campaigns() {
  const { user } = useAuth()
  const [list, setList] = useState(null)
  const [products, setProducts] = useState([])
  const [error, setError] = useState('')
  const [modal, setModal] = useState(null) // 'campaign' | 'product'
  const [toast, setToast] = useState('')
  const canEdit = user.role === 'admin' || user.role === 'marketing'

  const load = useCallback(async () => {
    try {
      const [c, p] = await Promise.all([api('GET', '/campaigns'), api('GET', '/products')])
      setList(c); setProducts(p); setError('')
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

function ProductModal({ onClose, onCreated }) {
  const [f, setF] = useState({ name: '', description: '', price: '', brand_info: '' })
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const set = (k) => (e) => setF({ ...f, [k]: e.target.value })
  async function submit(e) {
    e.preventDefault()
    setError(''); setBusy(true)
    try {
      const body = { name: f.name.trim(), description: f.description.trim(), brand_info: f.brand_info.trim() }
      if (f.price !== '') body.price = Number(f.price)
      await api('POST', '/products', body)
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
        <label className="field"><span>ข้อมูลแบรนด์ (ถ้ามี)</span><input value={f.brand_info} onChange={set('brand_info')} /></label>
        <div className="modal-foot">
          <button type="button" className="btn btn-quiet" onClick={onClose}>ย้อนกลับ</button>
          <button className="btn btn-primary" disabled={busy || !f.name.trim()}>{busy ? 'กำลังบันทึก…' : 'เพิ่มสินค้า'}</button>
        </div>
      </form>
    </Modal>
  )
}
