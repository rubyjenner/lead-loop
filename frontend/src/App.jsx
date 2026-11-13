import { useEffect, useState } from 'react'
import { api } from './api.js'
import { Navigate, NavLink, Outlet, Route, Routes, useLocation } from 'react-router-dom'
import { useAuth } from './auth.jsx'
import { LoopMark, ROLES, ThemeToggle } from './ui.jsx'
import { useLang } from './i18n.jsx'
import NotificationBell from './components/NotificationBell.jsx'
import Profile from './pages/Profile.jsx'
import Login from './pages/Login.jsx'
import SetPassword from './pages/SetPassword.jsx'
import ChangePassword from './pages/ChangePassword.jsx'
import Users from './pages/Users.jsx'
import Audit from './pages/Audit.jsx'
import Retention from './pages/Retention.jsx'
import Approvals from './pages/Approvals.jsx'
import Campaigns from './pages/Campaigns.jsx'
import CampaignDetail from './pages/CampaignDetail.jsx'
import Overview from './pages/Overview.jsx'
import Leads from './pages/Leads.jsx'
import LeadDetail from './pages/LeadDetail.jsx'
import Scoring from './pages/Scoring.jsx'
import Followups from './pages/Followups.jsx'
import ChatTest from './pages/ChatTest.jsx'
import Reports from './pages/Reports.jsx'
import PublicChat from './pages/PublicChat.jsx'
import Integrations from './pages/Integrations.jsx'

function Shell() {
  const { user, logout } = useAuth()
  const loc = useLocation()
  const { lang, setLang, t } = useLang()
  const [inbox, setInbox] = useState({})
  const [expanded, setExpanded] = useState({ campaigns: true, leads: true, insights: false, admin: false })
  useEffect(() => {
    if (!user || user.mustChange) return undefined
    let alive = true
    const load = () => api('GET', '/inbox').then((d) => alive && setInbox(d || {})).catch(() => {})
    load()
    const timer = setInterval(load, 15000)
    return () => { alive = false; clearInterval(timer) }
  }, [loc.pathname, user])
  useEffect(() => {
    const group = loc.pathname.startsWith('/campaign') || loc.pathname.startsWith('/approvals') ? 'campaigns'
      : ['/leads', '/followups', '/chat-test'].some((p) => loc.pathname.startsWith(p)) ? 'leads'
      : ['/scoring', '/reports'].some((p) => loc.pathname.startsWith(p)) ? 'insights'
      : ['/users', '/retention', '/audit', '/integrations'].some((p) => loc.pathname.startsWith(p)) ? 'admin' : null
    if (group) setExpanded((v) => ({ ...v, [group]: true }))
  }, [loc.pathname])
  if (!user) return <Navigate to="/login" replace state={{ from: loc.pathname }} />
  if (user.mustChange) return <Navigate to="/change-password" replace />
  const badge = { '/approvals': inbox.contents_to_approve, '/followups': inbox.followups_to_approve, '/leads': (inbox.leads_ready || 0) + (inbox.messages_unread || 0) }
  const initials = (user.name || user.email).trim().slice(0, 1).toUpperCase()
  const toggle = (key) => setExpanded((v) => ({ ...v, [key]: !v[key] }))
  const item = (to, label) => <NavLink key={to} to={to} className="nav-item">{t(to, label)}{badge[to] > 0 && <span className="nav-badge">{badge[to]}</span>}</NavLink>
  const group = (key, label, children) => <section className="nav-section" key={key}>
    <button className={`nav-group-toggle${expanded[key] ? ' expanded' : ''}`} onClick={() => toggle(key)} aria-expanded={!!expanded[key]}>
      <span>{label}</span><span className="nav-chevron">{expanded[key] ? '−' : '+'}</span>
    </button>
    {expanded[key] && <div className="nav-subitems">{children}</div>}
  </section>
  return (
    <div className="shell">
      <aside className="side">
        <div className="brand"><LoopMark size={26} /><span>LeadLoop</span></div>
        <nav aria-label="เมนูหลัก">
          {item('/overview', 'ภาพรวม')}
          {group('campaigns', t('nav.campaigns', 'แคมเปญและคอนเทนต์'), <>{item('/campaigns', 'แคมเปญ')}{item('/approvals', 'อนุมัติคอนเทนต์')}</>)}
          {group('leads', t('nav.leads', 'ลูกค้าและลีด'), <>{item('/leads', 'ลีด')}{item('/followups', 'ติดตามลีด')}{(user.role === 'admin' || user.role === 'marketing') && item('/chat-test', 'ทดลองแชตลูกค้า')}</>)}
          {group('insights', t('nav.insights', 'วิเคราะห์และตั้งค่า'), <>{(user.role === 'admin' || user.role === 'marketing') && item('/scoring', 'กฎคะแนน')}{item('/reports', 'รายงาน')}</>)}
          {user.role === 'admin' && group('admin', t('nav.admin', 'ผู้ดูแลระบบ'), <>{item('/users', 'ผู้ใช้')}{item('/retention', 'ระยะเก็บข้อมูล')}{item('/audit', 'บันทึกการใช้งาน')}{item('/integrations', 'การเชื่อมต่อระบบ')}</>)}
        </nav>
        <div className="me">
          <NavLink to="/profile" className="me-link" title={t('me.profile', 'โปรไฟล์และการตั้งค่า')}>
            <div className="avatar avatar-lg" aria-hidden="true">{initials}</div>
            <div className="me-text"><div className="me-name">{user.name || user.email}</div><div className="me-role">{ROLES[user.role]?.label || user.role}{user.job_title ? ` · ${user.job_title}` : ''}</div></div>
          </NavLink>
          <button className="btn btn-quiet" onClick={() => logout()}>{t('me.logout', 'ออกจากระบบ')}</button>
        </div>
      </aside>
      <main className="main">
        <div className="topbar">
          <div className="seg seg-sm" role="group" aria-label="ภาษา / Language"><button className={lang === 'th' ? 'seg-on' : ''} onClick={() => setLang('th')}>ไทย</button><button className={lang === 'en' ? 'seg-on' : ''} onClick={() => setLang('en')}>EN</button></div>
          <ThemeToggle compact />
          <NotificationBell />
          <NavLink to="/profile" className="icon-btn theme-icon" aria-label={t('me.profile', 'โปรไฟล์และการตั้งค่า')} title={t('me.profile', 'โปรไฟล์และการตั้งค่า')}>⚙</NavLink>
        </div>
        <Outlet />
      </main>
    </div>
  )
}

function Home() { return <Navigate to="/overview" replace /> }

export default function App() {
  const { ready } = useAuth()
  if (!ready) return <div className="boot" aria-busy="true" />
  return <Routes>
    <Route path="/login" element={<Login />} /><Route path="/set-password" element={<SetPassword />} /><Route path="/chat" element={<PublicChat />} /><Route path="/change-password" element={<ChangePassword />} />
    <Route element={<Shell />}>
      <Route path="/" element={<Home />} /><Route path="/profile" element={<Profile />} /><Route path="/users" element={<Users />} /><Route path="/audit" element={<Audit />} /><Route path="/retention" element={<Retention />} />
      <Route path="/integrations" element={<Integrations />} /><Route path="/campaigns" element={<Campaigns />} /><Route path="/campaigns/:id" element={<CampaignDetail />} /><Route path="/approvals" element={<Approvals />} /><Route path="/overview" element={<Overview />} />
      <Route path="/leads" element={<Leads />} /><Route path="/leads/:id" element={<LeadDetail />} /><Route path="/scoring" element={<Scoring />} /><Route path="/followups" element={<Followups />} /><Route path="/chat-test" element={<ChatTest />} /><Route path="/reports" element={<Reports />} />
    </Route>
    <Route path="*" element={<Navigate to="/" replace />} />
  </Routes>
}
