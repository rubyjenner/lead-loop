import { useCallback, useEffect, useState } from 'react'
import { api } from '../api.js'
import { useAuth } from '../auth.jsx'
import { EVENTS, Modal, RULE_CATEGORY, Toast, canManage, eventLabel, personLabel, timeAgo } from '../ui.jsx'
import { LevelTag } from './Leads.jsx'

const summary = (c) => {
  if (c && c.event && Object.keys(c).length === 1) return eventLabel(c.event)
  if (c && c.event && c.where) return `${eventLabel(c.event)} (มีเงื่อนไขเพิ่ม)`
  if (c && c.lead) return 'เงื่อนไขจากข้อมูลลีด (เช่น งบ)'
  return 'เงื่อนไขขั้นสูง'
}

export default function Scoring() {
  const { user } = useAuth()
  const [rules, setRules] = useState(null)
  const [settings, setSettings] = useState({})
  const [events, setEvents] = useState(Object.keys(EVENTS))
  const [error, setError] = useState('')
  const [edit, setEdit] = useState(null) // rule | 'new'
  const [toast, setToast] = useState('')
  const [dirty, setDirty] = useState(false)
  const manage = canManage(user)

  const load = useCallback(async () => {
    try {
      const [r, s, k] = await Promise.all([api('GET', '/scoring-rules'), api('GET', '/scoring-settings'), api('GET', '/event-keywords')])
      setRules(r); setSettings(s); setEvents([...new Set([...Object.keys(EVENTS), ...Object.keys(k)])]); setError('')
    } catch (e) { setError(e.message) }
  }, [])
  useEffect(() => { load() }, [load])

  async function toggle(r) {
    try { await api('PATCH', `/scoring-rules/${r.id}`, { active: !r.active }); setDirty(true); load() } catch (e) { setError(e.message) }
  }
  async function remove(r) {
    if (!confirm(`ลบกฎ "${r.name}" ใช่หรือไม่?`)) return
    try { const x = await api('DELETE', `/scoring-rules/${r.id}`); setDirty(true); setToast(x.soft_deleted ? 'กฎนี้เคยให้คะแนนแล้ว จึงปิดการใช้งานแทนการลบ' : 'ลบกฎแล้ว'); load() } catch (e) { setError(e.message) }
  }
  async function recalc() {
    try { const x = await api('POST', '/scoring-rules/recalculate'); setDirty(false); setToast(`คำนวณใหม่ ${x.recalculated}/${x.total} ลีด`) } catch (e) { setError(e.message) }
  }

  const groups = Object.keys(RULE_CATEGORY).map((k) => [k, (rules || []).filter((r) => r.category === k)]).filter(([, l]) => l.length)
  return (
    <div className="page">
      <header className="page-head">
        <div>
          <h1>กฎคะแนน</h1>
          <p className="muted">ทีมแก้กฎได้เองโดยไม่ต้องแก้โค้ด 4 กฎหลักจากพี่เลี้ยงปิดหรือลบไม่ได้ ส่วนกฎเสริมปิด แก้ และลบได้</p>
        </div>
        <div className="actions">
          {user.role === 'admin' && <button className="btn btn-quiet" onClick={recalc}>คำนวณคะแนนลีดทั้งหมดใหม่</button>}
          {manage && <button className="btn btn-primary" onClick={() => setEdit('new')}>เพิ่มกฎ</button>}
        </div>
      </header>
      {error && <div className="alert alert-error" role="alert">{error}</div>}
      {dirty && <div className="alert alert-warn">แก้กฎแล้ว แต่ลีดเดิมยังใช้คะแนนเก่า {user.role === 'admin' ? 'กด "คำนวณคะแนนลีดทั้งหมดใหม่" เมื่อตรวจกฎเรียบร้อย' : 'ให้แอดมินกดคำนวณใหม่เมื่อพร้อม'}</div>}
      <div className="stats">
        <div className="stat"><b>{settings.handoff_threshold ?? '-'}</b><span>คะแนนที่พร้อมส่งต่อเซลส์</span></div>
        <div className="stat"><b>{settings.level_warm_min ?? '-'}</b><span>คะแนนเริ่มระดับ Warm</span></div>
        <div className="stat"><b>{settings.followup_warm?.max ?? '-'}</b><span>ติดตาม Warm สูงสุดกี่ครั้ง (ทุก {settings.followup_warm?.every_days ?? '-'} วัน)</span></div>
        <div className="stat"><b>{settings.followup_cold?.max ?? '-'}</b><span>ติดตาม Cold สูงสุดกี่ครั้ง (ทุก {settings.followup_cold?.every_days ?? '-'} วัน)</span></div>
      </div>
      {!rules && !error && <div className="muted pad">กำลังโหลด…</div>}
      {rules && (
        <div className="table-wrap">
          <table className="table">
            <thead><tr><th>กฎ</th><th>เมื่อเกิดเหตุการณ์</th><th className="right">คะแนน</th><th>นับ</th><th>ใช้งาน</th><th>แก้ไขล่าสุด</th>{manage && <th />}</tr></thead>
            <tbody>
              {groups.map(([cat, list]) => [
                <tr key={cat} className="catrow"><td colSpan={manage ? 7 : 6}>{RULE_CATEGORY[cat]}</td></tr>,
                ...list.map((r) => (
                  <tr key={r.id} className={r.active ? '' : 'row-off'}>
                    <td>{r.name}{r.is_core && <span className="tag tag-warn" style={{ marginLeft: 8 }}>กฎหลัก</span>}{r.description && <div className="muted small">{r.description}</div>}</td>
                    <td>{summary(r.condition)}</td>
                    <td className={`right pts ${r.points < 0 ? 'pts-neg' : 'pts-pos'}`}>{r.points > 0 ? '+' : ''}{r.points}</td>
                    <td className="nowrap">{r.mode === 'each' ? `ทุกครั้ง${r.max_total ? ` (สูงสุด ${r.max_total})` : ''}` : 'ครั้งเดียว'}</td>
                    <td>
                      <label className="checks"><input type="checkbox" checked={r.active} disabled={!manage || r.is_core} onChange={() => toggle(r)} aria-label={`เปิดใช้ ${r.name}`} /></label>
                    </td>
                    <td className="small muted" title={r.updated_at || ''}>{r.updated_at ? <>{timeAgo(r.updated_at)}{r.updated_by && <div>{personLabel(r.updated_by_name, r.updated_by_title, r.updated_by)}</div>}</> : '-'}</td>
                    {manage && <td className="actions">
                      <button className="btn btn-quiet" onClick={() => setEdit(r)}>แก้ไข</button>
                      {!r.is_core && <button className="btn btn-quiet" onClick={() => remove(r)}>ลบ</button>}
                    </td>}
                  </tr>
                )),
              ])}
            </tbody>
          </table>
        </div>
      )}
      {manage && <Tester events={events} />}
      {edit && <RuleModal rule={edit === 'new' ? null : edit} events={events} onClose={() => setEdit(null)} onSaved={() => { setEdit(null); setDirty(true); setToast('บันทึกกฎแล้ว'); load() }} />}
      <Toast msg={toast} onDone={() => setToast('')} />
    </div>
  )
}

function RuleModal({ rule, events, onClose, onSaved }) {
  const simple = !rule || (rule.condition && rule.condition.event && Object.keys(rule.condition).length === 1)
  const [f, setF] = useState({
    name: rule?.name || '', description: rule?.description || '', category: rule?.category || 'intent', points: rule?.points ?? 10,
    mode: rule?.mode || 'once', max_total: rule?.max_total ?? '', event: rule?.condition?.event || events[0],
    advanced: !simple, json: simple ? '' : JSON.stringify(rule.condition, null, 2),
  })
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const set = (k) => (e) => setF({ ...f, [k]: e.target.value })
  async function submit(e) {
    e.preventDefault(); setError('')
    let condition
    if (f.advanced) { try { condition = JSON.parse(f.json) } catch { return setError('JSON ของเงื่อนไขไม่ถูกต้อง') } } else condition = { event: f.event }
    const body = { name: f.name.trim(), description: f.description.trim(), category: f.category, points: Number(f.points), mode: f.mode, condition }
    if (f.mode === 'each' && f.max_total !== '') body.max_total = Number(f.max_total)
    setBusy(true)
    try { await api(rule ? 'PATCH' : 'POST', rule ? `/scoring-rules/${rule.id}` : '/scoring-rules', body); onSaved() } catch (err) { setError(err.message) } finally { setBusy(false) }
  }
  return (
    <Modal title={rule ? 'แก้ไขกฎ' : 'เพิ่มกฎ'} onClose={onClose}>
      <form className="form" onSubmit={submit} noValidate>
        {error && <div className="alert alert-error" role="alert">{error}</div>}
        <label className="field"><span>ชื่อกฎ</span><input value={f.name} onChange={set('name')} autoFocus /></label>
        <label className="field"><span>หมวด</span><select value={f.category} onChange={set('category')}>{Object.entries(RULE_CATEGORY).map(([k, v]) => <option key={k} value={k}>{v}</option>)}</select></label>
        {!f.advanced ? (
          <label className="field"><span>เมื่อเกิดเหตุการณ์</span><select value={f.event} onChange={set('event')}>{events.map((k) => <option key={k} value={k}>{eventLabel(k)}</option>)}</select>
            <small><button type="button" className="linkbtn" onClick={() => setF({ ...f, advanced: true, json: JSON.stringify({ event: f.event }, null, 2) })}>ใช้เงื่อนไขขั้นสูง (JSON)</button></small></label>
        ) : (
          <label className="field"><span>เงื่อนไข (JSON)</span><textarea className="code" rows="6" value={f.json} onChange={set('json')} />
            <small>รองรับ event, where, lead, all, any, not และตัวดำเนินการ = != &gt; &gt;= &lt; &lt;= in contains ระบบไม่รันโค้ด</small></label>
        )}
        <label className="field"><span>คะแนน (ใส่ค่าลบเพื่อหักคะแนน)</span><input type="number" value={f.points} onChange={set('points')} /></label>
        <label className="field"><span>การนับ</span><select value={f.mode} onChange={set('mode')}><option value="once">ครั้งเดียวต่อลีด</option><option value="each">ทุกครั้งที่เกิดเหตุการณ์</option></select></label>
        {f.mode === 'each' && <label className="field"><span>คะแนนรวมสูงสุดจากกฎนี้ (ไม่บังคับ)</span><input type="number" min="0" value={f.max_total} onChange={set('max_total')} /></label>}
        <label className="field"><span>คำอธิบาย (ไม่บังคับ)</span><input value={f.description} onChange={set('description')} /></label>
        <div className="modal-foot">
          <button type="button" className="btn btn-quiet" onClick={onClose}>ยกเลิก</button>
          <button className="btn btn-primary" disabled={busy || !f.name.trim()}>{busy ? 'กำลังบันทึก…' : 'บันทึก'}</button>
        </div>
      </form>
    </Modal>
  )
}

function Tester({ events }) {
  const [picked, setPicked] = useState(['asked_price', 'asked_details'])
  const [budget, setBudget] = useState('9000')
  const [price, setPrice] = useState('12900')
  const [res, setRes] = useState(null)
  const [error, setError] = useState('')
  const toggle = (k) => setPicked(picked.includes(k) ? picked.filter((x) => x !== k) : [...picked, k])
  async function run() {
    setError('')
    try {
      const body = { events: picked.map((type) => ({ type })), lead: {}, }
      if (budget !== '') body.lead.budget = Number(budget)
      if (price !== '') body.product_price = Number(price)
      setRes(await api('POST', '/scoring-rules/test', body))
    } catch (e) { setError(e.message) }
  }
  return (
    <section className="panel" style={{ marginTop: 18 }}>
      <h2>ทดลองกฎ (ไม่บันทึกอะไร)</h2>
      <p className="muted small">เลือกเหตุการณ์ที่สมมติว่าลูกค้าทำ แล้วดูว่าได้กี่คะแนนและอยู่ระดับไหน ใช้ตรวจกฎก่อนแก้จริง</p>
      <div className="checks" style={{ marginBottom: 12 }}>
        {events.map((k) => <label key={k}><input type="checkbox" checked={picked.includes(k)} onChange={() => toggle(k)} />{eventLabel(k)}</label>)}
      </div>
      <div className="grid2">
        <label className="field"><span>งบของลูกค้า (บาท)</span><input type="number" value={budget} onChange={(e) => setBudget(e.target.value)} /></label>
        <label className="field"><span>ราคาสินค้า (บาท)</span><input type="number" value={price} onChange={(e) => setPrice(e.target.value)} /></label>
      </div>
      <button className="btn btn-primary" onClick={run}>ทดลองคำนวณ</button>
      {error && <div className="alert alert-error" role="alert" style={{ marginTop: 10 }}>{error}</div>}
      {res && (
        <div style={{ marginTop: 14 }}>
          <p><b className="pts" style={{ fontSize: 22 }}>{res.score}</b> คะแนน <LevelTag level={res.level} />{res.qualified && <span className="tag tag-l-qualified" style={{ marginLeft: 8 }}>{res.immediate_handoff ? 'ส่งต่อทันที' : 'ถึงเกณฑ์ส่งต่อ'}</span>}</p>
          <ul className="bd">
            {(res.breakdown || []).map((b) => <li key={b.rule_id}><span>{b.rule_name}{b.matches > 1 ? ` ×${b.matches}` : ''}</span><span className={`pts ${b.points < 0 ? 'pts-neg' : 'pts-pos'}`}>{b.points > 0 ? '+' : ''}{b.points}</span></li>)}
            {(!res.breakdown || res.breakdown.length === 0) && <li className="muted">ไม่มีกฎที่ตรง</li>}
          </ul>
        </div>
      )}
    </section>
  )
}
