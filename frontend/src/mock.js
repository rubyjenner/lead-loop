// In-memory fake API used by `npm run dev:mock`, so the screens can be viewed without the backend.
const now = Date.now()
const ago = (min) => new Date(now - min * 60000).toISOString()
let nextId = 8
let users = [
  { id: 1, email: 'admin@example.com', name: 'อาทิตยา เสาเวียง', role: 'admin', active: true, must_change_password: false, auth_provider: 'local', last_login_at: ago(3), created_at: ago(60 * 24 * 12), pending_link: false },
  { id: 2, email: 'napat.m@vannessplus.com', name: 'ณภัทร มั่นคง', role: 'marketing', active: true, must_change_password: false, auth_provider: 'local', last_login_at: ago(95), created_at: ago(60 * 24 * 9), pending_link: false },
  { id: 3, email: 'pimchanok.s@vannessplus.com', name: 'พิมพ์ชนก สุขใจ', role: 'marketing', active: true, must_change_password: false, auth_provider: 'local', last_login_at: ago(60 * 26), created_at: ago(60 * 24 * 9), pending_link: false },
  { id: 4, email: 'kritsada.t@vannessplus.com', name: 'กฤษดา ทองดี', role: 'sales', active: true, must_change_password: false, auth_provider: 'local', last_login_at: ago(60 * 24 * 3), created_at: ago(60 * 24 * 8), pending_link: false },
  { id: 5, email: 'wipada.r@vannessplus.com', name: 'วิภาดา รัตนา', role: 'sales', active: true, must_change_password: true, auth_provider: 'local', last_login_at: null, created_at: ago(60 * 5), pending_link: true },
  { id: 6, email: 'old.staff@vannessplus.com', name: 'สมศักดิ์ ใจดี', role: 'sales', active: false, must_change_password: false, auth_provider: 'local', last_login_at: ago(60 * 24 * 40), created_at: ago(60 * 24 * 60), pending_link: false },
  { id: 7, email: 'tester@vannessplus.com', name: '', role: 'marketing', active: true, must_change_password: true, auth_provider: 'local', last_login_at: null, created_at: ago(30), pending_link: true },
]
const audit = [
  ['login_success', 'admin@example.com', 'admin@example.com', 'ล็อกอินสำเร็จ'],
  ['user_created', 'admin@example.com', 'user:7', 'tester@vannessplus.com role=marketing (invite link)'],
  ['content_approve_denied', 'napat.m@vannessplus.com', 'content:12', 'approver is the creator'],
  ['content_approved', 'pimchanok.s@vannessplus.com', 'content:12', ''],
  ['login_failed', '', 'kritsada.t@vannessplus.com', ''],
  ['reset_link_created', 'admin@example.com', 'user:4', 'kritsada.t@vannessplus.com'],
  ['user_updated', 'admin@example.com', 'user:6', 'role sales->sales, active true->false'],
].map((r, i) => ({ id: 100 - i, at: ago(i * 47 + 2), actor_id: 1, actor_email: r[1], action: r[0], target: r[2], ip: '172.18.0.1', detail: r[3] }))

let contents = [
  { id: 14, campaign_id: 3, campaign_name: 'เปิดตัวหูฟังไร้สาย Aria', channel: 'facebook', status: 'pending_approval', generated_by: 'gemini', created_by: 'napat.m@vannessplus.com', approved_by: null, created_at: ago(55),
    body: 'หูฟังไร้สาย Aria เสียงใส แบตอึด 30 ชั่วโมง ราคา 2,490 บาท\nสั่งวันนี้ รับฟรีเคสพกพา ถึงสิ้นเดือนนี้เท่านั้น' },
  { id: 13, campaign_id: 3, campaign_name: 'เปิดตัวหูฟังไร้สาย Aria', channel: 'line', status: 'pending_approval', generated_by: 'human', created_by: 'admin@example.com', approved_by: null, created_at: ago(140),
    body: 'สนใจหูฟัง Aria ทักแชตนี้ได้เลยค่ะ ทีมงานพร้อมตอบเรื่องราคาและการจัดส่ง' },
  { id: 12, campaign_id: 2, campaign_name: 'ลดล้างสต็อกมือถือมือสอง', channel: 'facebook', status: 'pending_approval', generated_by: 'claude', created_by: 'pimchanok.s@vannessplus.com', approved_by: null, created_at: ago(60 * 5),
    body: 'มือถือมือสองสภาพดี แบตเตอรี่ 90% ราคา 12,900 บาท\nตรวจสภาพก่อนส่งทุกเครื่อง' },
  { id: 11, campaign_id: 2, campaign_name: 'ลดล้างสต็อกมือถือมือสอง', channel: 'line', status: 'draft', generated_by: 'template', created_by: 'napat.m@vannessplus.com', approved_by: null, created_at: ago(60 * 8),
    body: 'มือถือมือสอง ราคา 12,900 บาท ทักมาสอบถามรายละเอียดได้เลย' },
  { id: 10, campaign_id: 1, campaign_name: 'โปรโมชันต้นปี', channel: 'facebook', status: 'approved', generated_by: 'gemini', created_by: 'napat.m@vannessplus.com', approved_by: 'pimchanok.s@vannessplus.com', created_at: ago(60 * 30),
    body: 'โปรต้นปี ลด 15% สำหรับสมาชิกใหม่ ถึงสิ้นเดือนนี้' },
  { id: 9, campaign_id: 1, campaign_name: 'โปรโมชันต้นปี', channel: 'line', status: 'rejected', generated_by: 'claude', created_by: 'pimchanok.s@vannessplus.com', approved_by: null, created_at: ago(60 * 31),
    body: 'ลดแรงที่สุดในประเทศ! ห้ามพลาด' },
]
let products = [
  { id: 1, name: 'มือถือมือสอง Smoke Phone', description: 'สภาพดี แบตเตอรี่ 90% ตรวจสภาพก่อนส่ง', price: 12900, brand_info: '', created_at: ago(60 * 24 * 6) },
  { id: 2, name: 'หูฟังไร้สาย Aria', description: 'แบตอึด 30 ชั่วโมง กันน้ำ IPX4', price: 2490, brand_info: '', created_at: ago(60 * 24 * 4) },
]
let campaigns = [
  { id: 3, product_id: 2, name: 'เปิดตัวหูฟังไร้สาย Aria', goal: 'ได้ลีด 80 คนภายในเดือนนี้', target_audience: 'คนทำงานรุ่นใหม่ ชอบฟังเพลงระหว่างเดินทาง', budget: 20000, status: 'planned', created_at: ago(60 * 24 * 3), plan: 'เป้าหมาย: ได้ลีด 80 คนภายในเดือนนี้ ด้วยงบ 20,000 บาท\n\n1) สัปดาห์ที่ 1: โพสต์เปิดตัวใน Facebook เน้นแบตอึด 30 ชั่วโมงและราคา 2,490 บาท\n2) สัปดาห์ที่ 2: ข้อความ LINE ตอบคำถามราคาและการใช้งาน ให้ลูกค้าที่สนใจกดยินยอมก่อนคุย\n3) สัปดาห์ที่ 3–4: ติดตามลีดที่ถามราคา ส่งต่อเซลส์เมื่อคะแนนถึงเกณฑ์\n\nวัดผล: จำนวนลีดที่ยินยอม, อัตราตอบกลับ, ลีดที่ถูกส่งต่อ', plan_generated_by: 'gemini' },
  { id: 2, product_id: 1, name: 'ลดล้างสต็อกมือถือมือสอง', goal: 'ปิดการขาย 10 เครื่อง', target_audience: 'นักศึกษา', budget: 8000, status: 'draft', created_at: ago(60 * 24 * 5), plan: '', plan_generated_by: '' },
  { id: 1, product_id: null, name: 'โปรโมชันต้นปี', goal: '', target_audience: '', budget: 0, status: 'finished', created_at: ago(60 * 24 * 20), plan: '', plan_generated_by: '' },
]

let leadSeq = 20
const leads = [
  { id: 1, display_name: 'ลูกค้าทดสอบ 001', channel: 'mock', budget: 12900, status: 'qualified', score: 70, level: 'hot', consent_general: true, consent_marketing: true, created_at: ago(60 * 5), score_breakdown: [{ rule: 'ขอคุยกับเจ้าหน้าที่', points: 30 }, { rule: 'สอบถามราคา', points: 10 }, { rule: 'ทักขอรายละเอียด', points: 10 }, { rule: 'งบเพียงพอกับราคาสินค้า', points: 20 }] },
  { id: 2, display_name: 'ลูกค้าทดสอบ 002', channel: 'mock', budget: 9000, status: 'nurturing', score: 35, level: 'warm', consent_general: true, consent_marketing: true, created_at: ago(60 * 26), score_breakdown: [{ rule: 'สอบถามราคา', points: 10 }, { rule: 'ถามผ่อน', points: 15 }, { rule: 'ทักขอรายละเอียด', points: 10 }] },
  { id: 3, display_name: 'ลูกค้าทดสอบ 003', channel: 'line', budget: null, status: 'new', score: 10, level: 'cold', consent_general: true, consent_marketing: false, created_at: ago(60 * 50), score_breakdown: [{ rule: 'ทักขอรายละเอียด', points: 10 }] },
  { id: 4, display_name: 'ลูกค้าทดสอบ 004', channel: 'mock', budget: 15000, status: 'handed_off', score: 60, level: 'hot', consent_general: true, consent_marketing: false, created_at: ago(60 * 80), score_breakdown: [{ rule: 'ให้ติดต่อกลับ', points: 30 }, { rule: 'สอบถามราคา', points: 10 }, { rule: 'ระบุงบ', points: 20 }] },
  { id: 5, display_name: 'ลูกค้าทดสอบ 005', channel: 'web', budget: null, status: 'lost', score: 0, level: 'disqualified', consent_general: true, consent_marketing: false, created_at: ago(60 * 120), score_breakdown: [{ rule: 'ขอเลิกรับข่าวสาร', points: -100 }] },
]
let rules = [
  { id: 1, name: 'ทักขอรายละเอียด', description: 'ลูกค้าขอรายละเอียดสินค้า', category: 'intent', is_core: true, condition: { event: 'asked_details' }, points: 10, mode: 'once', max_total: null, active: true },
  { id: 2, name: 'ขอคุยกับเจ้าหน้าที่', description: 'ส่งต่อเซลส์ทันที', category: 'intent', is_core: true, condition: { event: 'requested_human' }, points: 30, mode: 'once', max_total: null, active: true },
  { id: 3, name: 'สอบถามราคา', description: '', category: 'intent', is_core: true, condition: { event: 'asked_price' }, points: 10, mode: 'once', max_total: null, active: true },
  { id: 4, name: 'ให้ติดต่อกลับ', description: 'ส่งต่อเซลส์ทันที', category: 'intent', is_core: true, condition: { event: 'requested_callback' }, points: 30, mode: 'once', max_total: null, active: true },
  { id: 5, name: 'ถามผ่อน', description: '', category: 'intent', is_core: false, condition: { event: 'asked_installment' }, points: 15, mode: 'once', max_total: null, active: true },
  { id: 6, name: 'งบเพียงพอกับราคาสินค้า', description: 'งบ ≥ ราคาสินค้า', category: 'fit', is_core: false, condition: { lead: { field: 'budget', op: '>=', value: 12900 } }, points: 20, mode: 'once', max_total: null, active: true },
  { id: 7, name: 'ตอบกลับเร็ว', description: '', category: 'engagement', is_core: false, condition: { event: 'replied_fast' }, points: 5, mode: 'each', max_total: 15, active: true },
  { id: 8, name: 'ไม่ตอบ 3 วัน', description: '', category: 'negative', is_core: false, condition: { event: 'no_reply_3d' }, points: -10, mode: 'once', max_total: null, active: false },
]
const settings = { handoff_threshold: 50, level_warm_min: 25, score_min: 0, score_max: 100, followup_cold: { every_days: 7, max: 2 }, followup_warm: { every_days: 3, max: 3 } }
const KW = { asked_details: ['รายละเอียด', 'สนใจ'], asked_price: ['ราคา', 'เท่าไหร่'], requested_human: ['คุยกับเจ้าหน้าที่'], requested_callback: ['โทรกลับ', 'ติดต่อกลับ'], asked_installment: ['ผ่อน'], unsubscribed: ['เลิกรับ'] }
const RULE_OF = { asked_details: 1, requested_human: 2, asked_price: 3, requested_callback: 4, asked_installment: 5 }
const level = (sc) => (sc >= 50 ? 'hot' : sc >= 25 ? 'warm' : 'cold')
const ok = (json, status = 200) => ({ status, json })
const err = (status, error, code) => ({ status, json: { error, code } })
const wait = (ms) => new Promise((r) => setTimeout(r, ms))

export async function mockRequest(method, path, body, token) {
  await wait(120)
  if (method === 'POST' && path === '/auth/login') {
    if (body.password === 'wrong') return err(401, 'invalid email or password')
    return ok({ token: 'mock-token', role: 'admin', expires_in: 28800, must_change_password: false })
  }
  if (method === 'POST' && path === '/auth/set-password') {
    if (body.token === 'expired') return err(400, 'link is invalid, expired or already used')
    return ok({ status: 'password set, you can log in now' })
  }
  if (!token) return err(401, 'unauthorized')
  if (path === '/auth/me') return ok({ id: '1', email: 'admin@example.com', role: 'admin', name: 'อาทิตยา เสาเวียง' })
  if (method === 'POST' && path === '/auth/password') return ok({ token: 'mock-token', expires_in: 28800 })
  if (method === 'GET' && path === '/users') return ok(users)
  if (method === 'GET' && path === '/users/approver-check') {
    const n = users.filter((u) => u.active && (u.role === 'marketing' || u.role === 'admin')).length
    return ok({ active_approvers: n, minimum: 2, ok: n >= 2, ...(n < 2 ? { warning: 'fewer than 2' } : {}) })
  }
  if (method === 'POST' && path === '/users') {
    if (users.some((u) => u.email === body.email)) return err(409, 'email already exists')
    const u = { id: nextId++, email: body.email, name: body.name || '', role: body.role, active: true, must_change_password: true, auth_provider: 'local', last_login_at: null, created_at: new Date().toISOString(), pending_link: !body.password }
    users.push(u)
    return ok({ id: u.id, email: u.email, name: u.name, role: u.role, must_change_password: true, invite_url: `http://localhost:5173/set-password?token=mockInviteToken${u.id}x9Qe2LmA7vT`, invite_expires_at: new Date(now + 72 * 3600e3).toISOString() }, 201)
  }
  let m = path.match(/^\/users\/(\d+)$/)
  if (method === 'PATCH' && m) {
    const u = users.find((x) => x.id === +m[1])
    if (!u) return err(404, 'user not found')
    if (u.role === 'admin' && (body.active === false || (body.role && body.role !== 'admin')) && users.filter((x) => x.role === 'admin' && x.active).length < 2) {
      return err(409, 'cannot demote or disable the last active admin')
    }
    Object.assign(u, body)
    return ok({ id: u.id, role: u.role, active: u.active })
  }
  m = path.match(/^\/users\/(\d+)\/reset-link$/)
  if (method === 'POST' && m) {
    const u = users.find((x) => x.id === +m[1])
    if (!u) return err(404, 'user not found')
    if (!u.active) return err(409, 'user is disabled')
    u.pending_link = true
    return ok({ reset_url: `http://localhost:5173/set-password?token=mockResetToken${u.id}b7Kd4WnP1zS`, expires_at: new Date(now + 3600e3).toISOString() }, 201)
  }
  if (method === 'GET' && path === '/campaigns') return ok(campaigns.map(({ plan, plan_generated_by, ...c }) => c))
  if (method === 'GET' && path === '/products') return ok(products)
  if (method === 'POST' && path === '/products') {
    const p = { id: products.length + 1, name: body.name, description: body.description || '', price: body.price ?? null, brand_info: body.brand_info || '', created_at: new Date().toISOString() }
    products.push(p); return ok(p, 201)
  }
  if (method === 'POST' && path === '/campaigns') {
    const c = { id: campaigns.length + 1, product_id: body.product_id ?? null, name: body.name, goal: body.goal || '', target_audience: body.target_audience || '', budget: body.budget || 0, status: 'draft', created_at: new Date().toISOString(), plan: '', plan_generated_by: '' }
    campaigns.unshift(c); return ok(c, 201)
  }
  m = path.match(/^\/campaigns\/(\d+)$/)
  if (method === 'GET' && m) {
    const c = campaigns.find((x) => x.id === +m[1])
    if (!c) return err(404, 'campaign not found')
    return ok({ ...c, product_name: products.find((p) => p.id === c.product_id)?.name ?? null })
  }
  m = path.match(/^\/campaigns\/(\d+)\/contents$/)
  if (method === 'GET' && m) return ok(contents.filter((c) => c.campaign_id === +m[1]))
  m = path.match(/^\/campaigns\/(\d+)\/plan$/)
  if (method === 'POST' && m) {
    const c = campaigns.find((x) => x.id === +m[1])
    c.plan = `เป้าหมาย: ${c.goal || 'ยังไม่ระบุ'} งบ ${c.budget} บาท\n\n1) ประกาศสินค้าและราคาจริงในช่องทางหลัก\n2) ตอบคำถามจากข้อมูลสินค้า ไม่แต่งข้อมูลเพิ่ม\n3) ติดตามลีดที่ยินยอม ส่งต่อเซลส์เมื่อถึงเกณฑ์`
    c.plan_generated_by = 'gemini'; if (c.status === 'draft') c.status = 'planned'
    return ok({ campaign_id: c.id, plan: c.plan, generated_by: 'gemini', fallback_reason: '', retry_queued: false })
  }
  m = path.match(/^\/campaigns\/(\d+)\/generate-contents$/)
  if (method === 'POST' && m) {
    const c = campaigns.find((x) => x.id === +m[1])
    const out = Array.from({ length: body.count }, (_, i) => ({ id: 100 + contents.length + i, campaign_id: c.id, campaign_name: c.name, channel: body.channel, status: 'draft', generated_by: 'gemini', created_by: 'admin@example.com', approved_by: null, created_at: new Date().toISOString(), body: `ร่างที่ ${i + 1}: ${c.name} ${products.find((p) => p.id === c.product_id)?.description || ''}` }))
    contents.unshift(...out)
    return ok({ generated_by: 'gemini', fallback_reason: '', retry_queued: false, contents: out }, 201)
  }
  if (method === 'GET' && path === '/ai-usage/me') return ok({ used_today: 4, per_user_daily_limit: 30, token_cap_daily: 0 })
  if (method === 'GET' && path.startsWith('/contents')) {
    const st = new URLSearchParams(path.split('?')[1] || '').get('status')
    return ok(contents.filter((c) => !st || c.status === st))
  }
  m = path.match(/^\/contents\/(\d+)\/(submit|approve|reject)$/)
  if (method === 'POST' && m) {
    const c = contents.find((x) => x.id === +m[1])
    if (!c) return err(409, 'content not found or not pending approval')
    if (m[2] === 'submit') { c.status = 'pending_approval'; return ok({ status: 'pending_approval' }) }
    if (c.status !== 'pending_approval') return err(409, 'content not found or not pending approval')
    if (m[2] === 'approve') {
      if (c.created_by === 'admin@example.com') return err(403, 'approver must be a different person from the content creator')
      c.status = 'approved'; c.approved_by = 'admin@example.com'; return ok({ status: 'approved' })
    }
    c.status = 'rejected'; return ok({ status: 'rejected' })
  }

  if (path.startsWith('/leads') || path.startsWith('/scoring') || path === '/event-keywords') {
    const role = 'admin'
    let mm
    if (method === 'GET' && path.startsWith('/leads?') || (method === 'GET' && path === '/leads')) {
      const q = new URLSearchParams(path.split('?')[1] || '')
      return ok(leads.filter((l) => (!q.get('level') || l.level === q.get('level')) && (!q.get('status') || l.status === q.get('status'))).sort((a, b) => b.score - a.score).map(({ score_breakdown, ...l }) => l))
    }
    if (method === 'POST' && path === '/leads') {
      if (!body.consent_general) return err(400, 'consent_general=true and consent_version are required before a lead can be stored')
      const l = { id: ++leadSeq, display_name: body.display_name, channel: body.channel, budget: body.budget ?? null, status: 'new', score: 0, level: 'cold', consent_general: true, consent_marketing: !!body.consent_marketing, created_at: new Date().toISOString(), score_breakdown: [] }
      leads.push(l); return ok({ id: l.id }, 201)
    }
    mm = path.match(/^\/leads\/(\d+)$/)
    if (mm) {
      const i = leads.findIndex((x) => x.id === +mm[1])
      if (i < 0) return err(404, 'lead not found')
      if (method === 'GET') return ok(leads[i])
      if (method === 'DELETE') { leads.splice(i, 1); return ok({ ok: true }) }
    }
    mm = path.match(/^\/leads\/(\d+)\/(message|status|consent)$/)
    if (mm && method === 'POST') {
      const l = leads.find((x) => x.id === +mm[1]); if (!l) return err(404, 'lead not found')
      if (mm[2] === 'status') { l.status = body.status; return ok({ status: l.status }) }
      if (mm[2] === 'consent') {
        if (body.kind === 'general' && body.action === 'withdrawn') { l.consent_general = false; l.consent_marketing = false; l.status = 'lost' } else if (body.kind === 'marketing') l.consent_marketing = body.action === 'granted'
        return ok({ ok: true })
      }
      const detected = Object.keys(KW).filter((e) => KW[e].some((w) => (body.text || '').includes(w)))
      detected.forEach((e) => {
        const r = rules.find((x) => x.id === RULE_OF[e]); if (!r || !r.active || l.score_breakdown.some((b) => b.rule === r.name)) return
        l.score += r.points; l.score_breakdown.push({ rule: r.name, points: r.points })
      })
      l.score = Math.max(0, Math.min(100, l.score)); l.level = level(l.score)
      if (l.score >= 50 && ['new', 'nurturing'].includes(l.status)) l.status = 'qualified'
      else if (l.score >= 25 && l.status === 'new') l.status = 'nurturing'
      return ok({ detected, score: l.score, level: l.level })
    }
    if (method === 'GET' && path === '/scoring-rules') return ok(rules)
    if (method === 'GET' && path === '/scoring-settings') return ok(settings)
    if (method === 'GET' && path === '/event-keywords') return ok(KW)
    if (method === 'POST' && path === '/scoring-rules/recalculate') return ok({ recalculated: leads.length, total: leads.length })
    if (method === 'POST' && path === '/scoring-rules/test') {
      const types = (body.events || []).map((e) => e.type)
      const breakdown = rules.filter((r) => r.active && r.condition.event && types.includes(r.condition.event)).map((r) => ({ rule_id: r.id, rule_name: r.name, matches: 1, points: r.points }))
      rules.filter((r) => r.active && r.condition.lead && body.lead?.budget != null && body.product_price != null && body.lead.budget >= body.product_price).forEach((r) => breakdown.push({ rule_id: r.id, rule_name: r.name, matches: 1, points: r.points }))
      const score = Math.max(0, Math.min(100, breakdown.reduce((a, b) => a + b.points, 0)))
      const imm = types.includes('requested_human') || types.includes('requested_callback')
      return ok({ score, level: level(score), qualified: score >= 50 || imm, immediate_handoff: imm, disqualified: false, breakdown })
    }
    if (method === 'POST' && path === '/scoring-rules') {
      const r = { id: 100 + rules.length, is_core: false, description: body.description || '', max_total: body.max_total ?? null, active: true, ...body }
      rules.push(r); return ok(r, 201)
    }
    mm = path.match(/^\/scoring-rules\/(\d+)$/)
    if (mm) {
      const r = rules.find((x) => x.id === +mm[1]); if (!r) return err(404, 'rule not found')
      if (method === 'PATCH') {
        if (r.is_core && body.active === false) return err(400, 'core rules cannot be disabled')
        Object.assign(r, body); return ok(r)
      }
      if (method === 'DELETE') {
        if (r.is_core) return err(400, 'core rules cannot be deleted')
        rules = rules.filter((x) => x.id !== r.id); return ok({ ok: true, soft_deleted: false })
      }
    }
  }
  if (method === 'GET' && path.startsWith('/audit-log')) return ok(audit)
  return err(404, 'not found')
}
