import { useCallback, useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { api } from '../api.js'
import { useAuth } from '../auth.jsx'
import { CAMPAIGN_STATUS, CHANNELS, CONTENT_STATUS, SOURCE, Toast, baht, timeAgo } from '../ui.jsx'

const FALLBACK = {
  quota: 'โควตา AI ของคุณวันนี้ใช้ครบแล้ว ระบบใช้เทมเพลตแทน (พรุ่งนี้ใช้ AI ได้อีก)',
  no_key: 'ยังไม่ได้ตั้งคีย์ AI ระบบใช้เทมเพลตแทน',
  unavailable: 'AI ใช้ไม่ได้ในตอนนี้ ระบบใช้เทมเพลตแทน',
}

export default function CampaignDetail() {
  const { id } = useParams()
  const { user } = useAuth()
  const canEdit = user.role === 'admin' || user.role === 'marketing'
  const [camp, setCamp] = useState(null)
  const [contents, setContents] = useState([])
  const [usage, setUsage] = useState(null)
  const [past, setPast] = useState(null) // totals of earlier campaigns, used by the next AI plan
  const [error, setError] = useState('')
  const [busy, setBusy] = useState('') // 'plan' | 'contents' | content id
  const [notice, setNotice] = useState(null) // {kind, text} result of the last AI call
  const [channel, setChannel] = useState('line')
  const [count, setCount] = useState(3)
  const [toast, setToast] = useState('')

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
    try { const r = await api('POST', `/campaigns/${id}/plan`); setNotice(describe(r, 'แผน')); await load() }
    catch (e) { setNotice({ kind: 'error', text: e.message }) } finally { setBusy('') }
  }
  async function makeContents() {
    setBusy('contents'); setNotice(null)
    try {
      const r = await api('POST', `/campaigns/${id}/generate-contents`, { channel, count: Number(count) })
      setNotice(describe(r, 'ร่าง')); await load()
    } catch (e) { setNotice({ kind: 'error', text: e.message }) } finally { setBusy('') }
  }
  async function submit(c) {
    setBusy(c.id)
    try { await api('POST', `/contents/${c.id}/submit`); setToast('ส่งขออนุมัติแล้ว'); await load() }
    catch (e) { setNotice({ kind: 'error', text: e.message }) } finally { setBusy('') }
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
          <p className="muted">{camp.product_name ? `สินค้า: ${camp.product_name}` : 'ยังไม่ได้ผูกสินค้า (AI จะเขียนแบบทั่วไป)'}</p>
        </div>
        <span className={`tag tag-${camp.status}`}>{CAMPAIGN_STATUS[camp.status] || camp.status}</span>
      </header>

      <dl className="facts">
        <div><dt>เป้าหมาย</dt><dd>{camp.goal || '-'}</dd></div>
        <div><dt>กลุ่มเป้าหมาย</dt><dd>{camp.target_audience || '-'}</dd></div>
        <div><dt>งบประมาณ</dt><dd>{baht(camp.budget)}</dd></div>
      </dl>

      {notice && <div className={`alert alert-${notice.kind === 'error' ? 'error' : notice.kind === 'warn' ? 'warn' : 'info'}`} role="status">{notice.text}</div>}

      <section className="block">
        <div className="block-head">
          <h2>แผนแคมเปญ</h2>
          {canEdit && (
            <button className="btn btn-primary" disabled={!!busy} onClick={makePlan}>
              {busy === 'plan' ? 'AI กำลังวางแผน…' : camp.plan ? 'ให้ AI วางแผนใหม่' : 'ให้ AI วางแผน'}
            </button>
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
        {camp.plan
          ? <><p className="plan">{camp.plan}</p><p className="muted small">{SOURCE[camp.plan_generated_by] || ''} · แผนใหม่จะเขียนทับแผนเดิม</p></>
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
            <li key={c.id} className="card">
              <div className="card-top">
                <span className="tag">{CHANNELS[c.channel] || c.channel}</span>
                <span className={`tag tag-c-${c.status}`}>{CONTENT_STATUS[c.status] || c.status}</span>
                <span className={`src src-${c.generated_by}`}>{SOURCE[c.generated_by] || c.generated_by}</span>
              </div>
              <p className="card-body">{c.body}</p>
              <div className="card-meta muted small">สร้างโดย {c.created_by || 'ไม่ระบุ'} · {timeAgo(c.created_at)}</div>
              {canEdit && (c.status === 'draft' || c.status === 'rejected') && (
                <div className="card-actions">
                  <button className="btn btn-primary" disabled={!!busy} onClick={() => submit(c)}>{busy === c.id ? 'กำลังส่ง…' : c.status === 'rejected' ? 'ส่งขออนุมัติอีกครั้ง' : 'ส่งขออนุมัติ'}</button>
                </div>
              )}
              {c.status === 'pending_approval' && (
                <div className="card-actions"><Link to="/approvals" className="link">ไปหน้าอนุมัติ</Link><span className="muted small">ผู้อนุมัติต้องเป็นคนอื่นที่ไม่ใช่ผู้สร้าง</span></div>
              )}
            </li>
          ))}
        </ul>
      </section>
      <Toast msg={toast} onDone={() => setToast('')} />
    </div>
  )
}
