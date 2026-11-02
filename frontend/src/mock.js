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
  if (method === 'GET' && path.startsWith('/audit-log')) return ok(audit)
  return err(404, 'not found')
}
