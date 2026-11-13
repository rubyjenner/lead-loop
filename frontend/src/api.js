// Thin client for the LeadLoop API. All calls go to /api/... (proxied to the Go API).
import { mockRequest } from './mock.js'

const MOCK = import.meta.env.MODE === 'mock'
const KEY = 'leadloop.token'

export function getToken() {
  try { return sessionStorage.getItem(KEY) || '' } catch { return '' }
}
export function setToken(t) {
  try { t ? sessionStorage.setItem(KEY, t) : sessionStorage.removeItem(KEY) } catch { /* private window */ }
}

let onUnauthorized = () => {}
export function setUnauthorizedHandler(fn) { onUnauthorized = fn }

export class ApiError extends Error {
  constructor(status, message, code) { super(message); this.status = status; this.code = code }
}

// Messages for the errors people actually meet. Anything else shows what the server said.
const MESSAGES = {
  'invalid email or password': 'อีเมลหรือรหัสผ่านไม่ถูกต้อง',
  'too many failed attempts, try again in 15 minutes': 'ลองผิดหลายครั้งเกินไป รอ 15 นาทีแล้วลองใหม่',
  'email already exists': 'อีเมลนี้มีบัญชีอยู่แล้ว',
  'email domain is not allowed': 'โดเมนอีเมลนี้ไม่ได้รับอนุญาต',
  'valid email is required': 'กรอกอีเมลให้ถูกต้อง',
  'cannot demote or disable the last active admin': 'ต้องมีแอดมินที่ใช้งานอยู่อย่างน้อย 1 คน',
  'link is invalid, expired or already used': 'ลิงก์นี้ใช้ไม่ได้แล้ว (หมดอายุหรือถูกใช้ไปแล้ว) ขอลิงก์ใหม่จากแอดมิน',
  'new password must be 8-72 characters': 'รหัสผ่านใหม่ต้องยาว 8–72 ตัวอักษร',
  'current password is incorrect': 'รหัสผ่านปัจจุบันไม่ถูกต้อง',
  'new password must be different from the current one': 'รหัสผ่านใหม่ต้องไม่ซ้ำกับรหัสเดิม',
  'approver must be a different person from the content creator': 'อนุมัติเองไม่ได้ ต้องให้คนอื่นที่ไม่ใช่ผู้สร้างเป็นผู้อนุมัติ',
  'content not found or not pending approval': 'คอนเทนต์นี้ไม่ได้อยู่ในสถานะรออนุมัติแล้ว (อาจมีคนจัดการไปก่อนหน้า) รีเฟรชรายการ',
  'content not found or not in draft/rejected status': 'ส่งขออนุมัติไม่ได้ คอนเทนต์นี้ไม่ได้อยู่ในสถานะร่างหรือถูกปฏิเสธ',
  'forbidden for your role': 'บทบาทของคุณทำรายการนี้ไม่ได้',
  'user is disabled': 'บัญชีนี้ถูกปิดใช้งานอยู่',
}

export async function api(method, path, body) {
  let status, json
  if (MOCK) {
    ;({ status, json } = await mockRequest(method, path, body, getToken()))
  } else {
    const headers = {}
    const t = getToken()
    if (t) headers.Authorization = `Bearer ${t}`
    if (body !== undefined) headers['Content-Type'] = 'application/json'
    let res
    try {
      res = await fetch(`/api${path}`, { method, headers, body: body === undefined ? undefined : JSON.stringify(body) })
    } catch {
      throw new ApiError(0, 'เชื่อมต่อเซิร์ฟเวอร์ไม่ได้ ตรวจว่า API ทำงานอยู่ (docker compose ps)')
    }
    status = res.status
    json = await res.json().catch(() => null)
  }
  if (status >= 200 && status < 300) return json
  if (status === 401 && path !== '/auth/login' && path !== '/auth/set-password') onUnauthorized()
  const raw = json?.error || `เกิดข้อผิดพลาด (${status})`
  throw new ApiError(status, MESSAGES[raw] || raw, json?.code)
}

export async function streamNotifications(onEvent, signal) {
  const headers = {}
  const token = getToken()
  if (token) headers.Authorization = `Bearer ${token}`
  const res = await fetch('/api/notifications/stream', { headers, signal })
  if (!res.ok || !res.body) throw new Error(`notification stream failed (${res.status})`)
  const reader = res.body.getReader()
  const decoder = new TextDecoder()
  let buffer = ''
  while (true) {
    const { value, done } = await reader.read()
    if (done) break
    buffer += decoder.decode(value, { stream: true })
    let boundary
    while ((boundary = buffer.indexOf('\n\n')) >= 0) {
      const frame = buffer.slice(0, boundary)
      buffer = buffer.slice(boundary + 2)
      if (frame.split('\n').some((line) => line.trim() === 'event: notification')) onEvent()
    }
  }
}
