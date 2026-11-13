import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, streamNotifications } from '../api.js'
import { useLang } from '../i18n.jsx'

function ago(value, lang) {
  if (!value) return ''
  const mins = Math.max(0, Math.floor((Date.now() - new Date(value).getTime()) / 60000))
  if (mins < 1) return lang === 'en' ? 'Just now' : 'เมื่อสักครู่'
  if (mins < 60) return lang === 'en' ? `${mins}m ago` : `${mins} นาทีที่แล้ว`
  const hours = Math.floor(mins / 60)
  if (hours < 24) return lang === 'en' ? `${hours}h ago` : `${hours} ชั่วโมงที่แล้ว`
  const days = Math.floor(hours / 24)
  return lang === 'en' ? `${days}d ago` : `${days} วันที่แล้ว`
}

export default function NotificationBell() {
  const { lang } = useLang()
  const [open, setOpen] = useState(false)
  const [data, setData] = useState({ unread_count: 0, items: [] })
  const [busy, setBusy] = useState(false)
  async function load() {
    try { setData(await api('GET', '/notifications?limit=30')) } catch { /* bell stays quiet if API is unavailable */ }
  }
  useEffect(() => {
    load()
    const controller = new AbortController()
    let retryTimer
    let active = true
    const connect = async () => {
      try { await streamNotifications(load, controller.signal) } catch { /* fall back to periodic refresh if stream drops */ }
      if (active && !controller.signal.aborted) retryTimer = setTimeout(connect, 2500)
    }
    connect()
    const timer = setInterval(load, 30000)
    return () => { active = false; controller.abort(); clearTimeout(retryTimer); clearInterval(timer) }
  }, [])
  async function markRead(id) {
    try { await api('POST', `/notifications/${id}/read`); await load() } catch { /* keep panel usable */ }
  }
  async function markAll() {
    setBusy(true)
    try { await api('POST', '/notifications/read-all'); await load() } catch { /* retry on next poll */ }
    finally { setBusy(false) }
  }
  return (
    <div className="notification-wrap">
      <button className="icon-btn theme-icon notification-trigger" aria-label={lang === 'en' ? 'Notifications' : 'การแจ้งเตือน'} title={lang === 'en' ? 'Notifications' : 'การแจ้งเตือน'} aria-expanded={open} onClick={() => { setOpen((v) => !v); if (!open) load() }}>
        <span aria-hidden="true">🔔</span>
        {data.unread_count > 0 && <span className="notification-count">{data.unread_count > 99 ? '99+' : data.unread_count}</span>}
      </button>
      {open && <>
        <button className="notification-dismiss" aria-label="Close notifications" onClick={() => setOpen(false)} />
        <section className="notification-panel" aria-label={lang === 'en' ? 'Notification history' : 'ประวัติการแจ้งเตือน'}>
          <header className="notification-head">
            <strong>{lang === 'en' ? 'Notifications' : 'การแจ้งเตือน'}</strong>
            <button className="btn btn-quiet small" disabled={!data.unread_count || busy} onClick={markAll}>{lang === 'en' ? 'Mark all read' : 'อ่านทั้งหมด'}</button>
          </header>
          {data.items.length === 0 ? <p className="notification-empty">{lang === 'en' ? 'No notifications yet' : 'ยังไม่มีการแจ้งเตือน'}</p> :
            <div className="notification-list">{data.items.map((item) => (
              <Link key={item.id} to={item.href || '/overview'} className={`notification-item${item.read_at ? '' : ' unread'}`} onClick={() => { markRead(item.id); setOpen(false) }}>
                <span className="notification-dot" />
                <span className="notification-copy"><strong>{item.title}</strong>{item.body && <span>{item.body}</span>}<small>{ago(item.created_at, lang)}</small></span>
              </Link>
            ))}</div>}
        </section>
      </>}
    </div>
  )
}
