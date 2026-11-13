import { useCallback, useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { api } from '../api.js'
import { useAuth } from '../auth.jsx'
import { CAMPAIGN_STATUS, CHANNELS, SOURCE, Toast, baht, canEditCampaign, impactNote, personLabel, timeAgo } from '../ui.jsx'
import CampaignProgress from './CampaignProgress.jsx'
import ContentCard from './ContentCard.jsx'
import ImagePicker from './ImagePicker.jsx'
import { FactsModal } from './Campaigns.jsx'

const FALLBACK = {
  quota: 'โควตา AI ของคุณวันนี้ใช้ครบแล้ว ระบบใช้เทมเพลตแทน (พรุ่งนี้ใช้ AI ได้อีก)',
  no_key: 'ยังไม่ได้ตั้งคีย์ AI ระบบใช้เทมเพลตแทน',
  unavailable: 'AI ใช้ไม่ได้ในตอนนี้ ระบบใช้เทมเพลตแทน',
}

export default function CampaignDetail() {
  const { id } = useParams()
  const { user } = useAuth()
  const [camp, setCamp] = useState(null)
  const canEdit = canEditCampaign(user, camp?.created_by) // the creator or an admin
  const [contents, setContents] = useState([])
  const [usage, setUsage] = useState(null)
  const [past, setPast] = useState(null) // totals of earlier campaigns, used by the next AI plan
  const [error, setError] = useState('')
  const [busy, setBusy] = useState('') // 'plan' | 'contents' | content id
  const [notice, setNotice] = useState(null) // {kind, text} result of the last AI call
  const [channel, setChannel] = useState('line')
  const [count, setCount] = useState(3)
  const [toast, setToast] = useState('')
  const [aiImage, setAiImage] = useState('') // optional picture for the next AI request only
  const [editProduct, setEditProduct] = useState(null)
  const [editingPlan, setEditingPlan] = useState(false)
  const [planText, setPlanText] = useState('')

  const load = useCallback(async () => {
    try {
      const [c, list, u] = await Promise.all([
        api('GET', `/campaigns/${id}`), api('GET', `/campaigns/${id}/contents`), api('GET', '/ai-usage/me'),
      ])
      setCamp(c); setContents(list); setUsage(u); setError('')
      api('GET', `/campaigns/${id}/past-results`).then(setPast).catch(() => setPast(null))
    } catch (e) { setError(e.message) }
  }, [id])
  useEffect(() => { load() }, [load])

  function describe(r, what) {
    if (r.generated_by === 'template') {
      const why = FALLBACK[r.fallback_reason] || FALLBACK.unavailable
      return { kind: 'warn', text: `${why}${r.retry_queued ? ' ระบบจะลองให้ AI เขียนใหม่เองภายหลัง และอัปเกรด${what}ให้ถ้าคุณยังไม่ได้แก้' : ''}` }
    }
    return { kind: 'ok', text: `${SOURCE[r.generated_by] || r.generated_by} เรียบร้อย` }
  }

  async function makePlan() {
    setBusy('plan'); setNotice(null)
    try { const r = await api('POST', `/campaigns/${id}/plan`, aiImage ? { image: aiImage } : undefined); setNotice(describe(r, 'แผน')); await load() }
    catch (e) { setNotice({ kind: 'error', text: e.message }) } finally { setBusy('') }
  }
  async function makeContents() {
    setBusy('contents'); setNotice(null)
    try {
      const r = await api('POST', `/campaigns/${id}/generate-contents`, { channel, count: Number(count), ...(aiImage ? { image: aiImage } : {}) })
      setNotice(describe(r, 'ร่าง')); await load()
    } catch (e) { setNotice({ kind: 'error', text: e.message }) } finally { setBusy('') }
  }
  async function savePlan() {
    setBusy('saveplan'); setNotice(null)
    try { await api('PUT', `/campaigns/${id}/plan`, { plan: planText }); setEditingPlan(false); setToast('บันทึกแผนแล้ว'); await load() }
    catch (e) { setNotice({ kind: 'error', text: e.message }) } finally { setBusy('') }
  }
  async function openProduct() {
    try {
      const [p, facts] = await Promise.all([api('GET', '/products'), api('GET', '/product-facts').catch(() => [])])
      const x = p.find((q) => q.id === camp.product_id)
      if (x) setEditProduct({ ...x, ...(facts.find((f) => f.id === x.id) || {}) })
    } catch (e) { setNotice({ kind: 'error', text: e.message }) }
  }

  if (error) return <div className="page"><div className="alert alert-error" role="alert">{error}</div><Link to="/campaigns" className="link">กลับไปรายการแคมเปญ</Link></div>
  if (!camp) return <div className="page muted">กำลังโหลด…</div>

  const left = usage ? Math.max(0, usage.per_user_daily_limit - usage.used_today) : null
  return (
    <div className="page">
      <p className="crumb"><Link to="/campaigns" className="link">แคมเปญ</Link> / {camp.name}</p>
      <header className="page-head">
        <div>
          <h1>{camp.name}</h1>
          <div className="prodcell">
            {camp.product_image_url && <img className="prod-thumb" src={camp.product_image_url} alt="" onError={(e) => { e.currentTarget.style.display = 'none' }} />}
            <span className="muted">
              {camp.product_name ? `สินค้า: ${camp.product_name}` : 'ยังไม่ได้ผูกสินค้า (AI จะเขียนแบบทั่วไป)'}
              
            </span>
          </div>
          <p className="muted small">สร้างโดย {personLabel(camp.created_by_name, camp.created_by_title, camp.created_by, camp.created_by_role)} · {timeAgo(camp.created_at)}</p>
        </div>
        <div className="actions">
          {canEdit && camp.product_id && <button className="btn btn-quiet" onClick={openProduct}>แก้ไขสินค้า</button>}
          <span className={`tag tag-${camp.status}`}>{CAMPAIGN_STATUS[camp.status] || camp.status}</span>
        </div>
      </header>

      <CampaignProgress id={id} version={`${contents.length}-${contents.map((x) => x.status).join()}-${camp.plan ? 1 : 0}`} />

      <dl className="facts">
        <div><dt>เป้าหมาย</dt><dd>{camp.goal || '-'}</dd></div>
        <div><dt>กลุ่มเป้าหมาย</dt><dd>{camp.target_audience || '-'}</dd></div>
        <div><dt>งบประมาณ</dt><dd>{baht(camp.budget)}</dd></div>
      </dl>

      {!canEdit && (
        <div className="alert alert-info" role="note">
          {user.role === 'sales' ? 'เซลส์ดูแคมเปญได้อย่างเดียว' : 'คุณไม่ใช่ผู้สร้างแคมเปญนี้ จึงแก้ไขไม่ได้ ถ้าเป็นผู้อนุมัติ คุณอนุมัติหรือปฏิเสธคอนเทนต์ที่รออนุมัติได้ในแต่ละการ์ดด้านล่าง'}
        </div>
      )}
      {canEdit && user.role === 'admin' && camp.created_by && camp.created_by.toLowerCase() !== user.email.toLowerCase() && (
        <div className="alert alert-warn" role="note">คุณเป็นแอดมินที่แก้แคมเปญของคนอื่น ทุกการแก้ไขจะถูกบันทึกในบันทึกการใช้งาน</div>
      )}
      {canEdit && (
        <ImagePicker value={aiImage} onChange={setAiImage} label="แนบรูปให้ AI ดูรอบนี้ (ไม่บังคับ)"
          hint={camp.product_image_url ? 'ถ้าไม่แนบ AI จะใช้รูปสินค้าที่อัปโหลดไว้ รูปที่แนบใช้เฉพาะครั้งนี้ ไม่ถูกบันทึก' : 'รูปที่แนบใช้เฉพาะครั้งนี้ ไม่ถูกบันทึก และจะถูกส่งให้ผู้ให้บริการ AI'} />
      )}
      {notice && <div className={`alert alert-${notice.kind === 'error' ? 'error' : notice.kind === 'warn' ? 'warn' : 'info'}`} role="status">{notice.text}</div>}

      <section className="block">
        <div className="block-head">
          <h2>แผนแคมเปญ</h2>
          {canEdit && !editingPlan && (
            <div className="genbar">
              {camp.plan && <button className="btn btn-quiet" disabled={!!busy} onClick={() => { setPlanText(camp.plan); setEditingPlan(true) }}>แก้แผนเอง</button>}
              <button className="btn btn-primary" disabled={!!busy} onClick={makePlan}>
                {busy === 'plan' ? 'AI กำลังวางแผน…' : camp.plan ? 'ให้ AI วางแผนใหม่' : 'ให้ AI วางแผน'}
              </button>
            </div>
          )}
        </div>
        {past && (
          <div className="alert alert-info" role="note">
            {past.enough
              ? <><strong>AI จะใช้ผลของแคมเปญรอบก่อนประกอบการวางแผน</strong> (ตัวเลขรวมเท่านั้น ไม่มีข้อมูลลูกค้า)
                <pre className="plan" style={{ margin: '6px 0 0', whiteSpace: 'pre-wrap' }}>{past.summary}</pre></>
              : <>ยังมีลีดจากแคมเปญอื่นไม่ถึง {past.min_leads} ราย แผนนี้จึงยังไม่อิงผลรอบก่อน</>}
          </div>
        )}
        {editingPlan ? (
          <div className="ad-edit">
            <textarea rows="12" maxLength={8000} value={planText} onChange={(e) => setPlanText(e.target.value)} aria-label="แผนแคมเปญ" />
            <p className="muted small">ไม่ชอบที่ AI เขียน แก้ได้เลย แผนที่คุณแก้จะไม่ถูกเขียนทับเอง (ยกเว้นกด "ให้ AI วางแผนใหม่")</p>
            <div className="card-actions">
              <button className="btn btn-primary" disabled={!!busy || !planText.trim()} onClick={savePlan}>{busy === 'saveplan' ? 'กำลังบันทึก…' : 'บันทึกแผน'}</button>
              <button className="btn btn-quiet" disabled={!!busy} onClick={() => setEditingPlan(false)}>ยกเลิก</button>
            </div>
          </div>
        ) : camp.plan
          ? <><p className="plan">{camp.plan}</p><p className="muted small">{SOURCE[camp.plan_generated_by] || ''} · "ให้ AI วางแผนใหม่" จะเขียนทับแผนเดิม</p></>
          : <p className="muted">ยังไม่มีแผน กดปุ่มด้านบนให้ AI ร่างแผนจากสินค้า เป้าหมาย และงบ ใช้เวลาไม่กี่วินาที</p>}
      </section>

      <section className="block">
        <div className="block-head">
          <h2>คอนเทนต์ ({contents.length})</h2>
          {canEdit && (
            <div className="genbar">
              <select value={channel} onChange={(e) => setChannel(e.target.value)} aria-label="ช่องทาง">
                {Object.entries(CHANNELS).map(([k, v]) => <option key={k} value={k}>{v}</option>)}
              </select>
              <select value={count} onChange={(e) => setCount(e.target.value)} aria-label="จำนวนร่าง">
                {[1, 2, 3, 4, 5].map((n) => <option key={n} value={n}>{n} ร่าง</option>)}
              </select>
              <button className="btn btn-primary" disabled={!!busy} onClick={makeContents}>{busy === 'contents' ? 'AI กำลังร่าง…' : 'ให้ AI ร่างคอนเทนต์'}</button>
            </div>
          )}
        </div>
        {canEdit && usage && <p className="muted small">วันนี้ใช้ AI ไปแล้ว {usage.used_today} จาก {usage.per_user_daily_limit} ครั้ง (เหลือ {left}) ถ้าครบ ระบบจะใช้เทมเพลตแทน</p>}
        {contents.length === 0 && <p className="muted">ยังไม่มีคอนเทนต์ ให้ AI ร่างหลายแบบ แล้วส่งขออนุมัติเฉพาะแบบที่ใช้ได้</p>}
        <ul className="cards">
          {contents.map((c) => (
            <ContentCard key={c.id} c={c} image={camp.product_image_url} campaignCreator={camp.created_by} productChangedAt={camp.product_changed_at} user={user}
              onError={(t) => setNotice(t ? { kind: 'error', text: t } : null)}
              onChanged={async (msg) => { if (msg) setToast(msg); await load() }} />
          ))}
        </ul>
      </section>
      {editProduct && <FactsModal product={editProduct} onClose={() => setEditProduct(null)} onSaved={async (r) => { setEditProduct(null); const n = impactNote(r?.changed, r?.impact); if (n) setNotice({ kind: 'warn', text: n }); else setToast('บันทึกข้อมูลสินค้าแล้ว (ไม่มีอะไรเปลี่ยน)'); await load() }} />}
      <Toast msg={toast} onDone={() => setToast('')} />
    </div>
  )
}
