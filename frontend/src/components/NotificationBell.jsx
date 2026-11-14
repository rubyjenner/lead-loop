import { useEffect, useRef, useState } from 'react'
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

// A short two-tone chime made with the Web Audio API (no sound file). Browsers only allow sound after the person has
// clicked or typed on the page once, so the audio context is created on the first interaction.
const SOUND_KEY = 'leadloop-bell-sound'
let audioCtx
function unlockAudio() {
  try {
    const AC = window.AudioContext || window.webkitAudioContext
    if (!AC) return
    audioCtx = audioCtx || new AC()
    if (audioCtx.state === 'suspended') audioCtx.resume()
  } catch { /* no sound available */ }
}
function chime() {
  try {
    unlockAudio()
    if (!audioCtx || audioCtx.state !== 'running') return
    const t0 = audioCtx.currentTime
    ;[[880, 0], [1320, 0.16]].forEach(([freq, at]) => {
      const osc = audioCtx.createOscillator()
      const gain = audioCtx.createGain()
      osc.type = 'sine'
      osc.frequency.value = freq
      gain.gain.setValueAtTime(0.0001, t0 + at)
      gain.gain.exponentialRampToValueAtTime(0.18, t0 + at + 0.02)
      gain.gain.exponentialRampToValueAtTime(0.0001, t0 + at + 0.32)
      osc.connect(gain).connect(audioCtx.destination)
      osc.start(t0 + at)
      osc.stop(t0 + at + 0.34)
    })
  } catch { /* ignore */ }
}
function soundOn() { try { return localStorage.getItem(SOUND_KEY) !== 'off' } catch { return true } }

export default function NotificationBell() {
  const { lang } = useLang()
  const [open, setOpen] = useState(false)
  const [data, setData] = useState({ unread_count: 0, items: [] })
  const [busy, setBusy] = useState(false)
  const [sound, setSound] = useState(soundOn)
  const lastUnread = useRef(null) // null until the first load, so opening the page never plays a sound
  const soundRef = useRef(sound)
  soundRef.current = sound
  async function load() {
    try {
      const d = await api('GET', '/notifications?limit=30')
      if (lastUnread.current !== null && d.unread_count > lastUnread.current && soundRef.current) chime()
      lastUnread.current = d.unread_count
      setData(d)
    } catch { /* bell stays quiet if API is unavailable */ }
  }
  function toggleSound() {
    const next = !sound
    setSound(next)
    try { localStorage.setItem(SOUND_KEY, next ? 'on' : 'off') } catch { /* ignore */ }
    if (next) chime() // lets the person hear what it sounds like
  }
  useEffect(() => {
    window.addEventListener('pointerdown', unlockAudio, { once: true })
    window.addEventListener('keydown', unlockAudio, { once: true })
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
            <button className="btn btn-quiet small" aria-pressed={sound} title={lang === 'en' ? 'Sound for new notifications' : 'เสียงเมื่อมีแจ้งเตือนใหม่'} onClick={toggleSound}>{sound ? '🔊' : '🔇'} {lang === 'en' ? (sound ? 'Sound on' : 'Sound off') : (sound ? 'เสียงเปิด' : 'เสียงปิด')}</button>
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
