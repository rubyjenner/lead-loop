import { Navigate, NavLink, Outlet, Route, Routes, useLocation } from 'react-router-dom'
import { useAuth } from './auth.jsx'
import { LoopMark, ROLES } from './ui.jsx'
import Login from './pages/Login.jsx'
import SetPassword from './pages/SetPassword.jsx'
import ChangePassword from './pages/ChangePassword.jsx'
import Users from './pages/Users.jsx'
import Audit from './pages/Audit.jsx'
import Soon from './pages/Soon.jsx'
import Approvals from './pages/Approvals.jsx'
import Campaigns from './pages/Campaigns.jsx'
import CampaignDetail from './pages/CampaignDetail.jsx'
import Overview from './pages/Overview.jsx'
import Leads from './pages/Leads.jsx'
import LeadDetail from './pages/LeadDetail.jsx'
import Scoring from './pages/Scoring.jsx'

const WORK = [
  ['/overview', 'ภาพรวม', true],
  ['/campaigns', 'แคมเปญ', true],
  ['/approvals', 'อนุมัติคอนเทนต์', true],
  ['/leads', 'ลีด', true],
  ['/scoring', 'กฎคะแนน', true],
]

function Shell() {
  const { user, logout } = useAuth()
  const loc = useLocation()
  if (!user) return <Navigate to="/login" replace state={{ from: loc.pathname }} />
  if (user.mustChange) return <Navigate to="/change-password" replace />
  const initials = (user.name || user.email).trim().slice(0, 1).toUpperCase()
  return (
    <div className="shell">
      <aside className="side">
        <div className="brand"><LoopMark size={26} /><span>LeadLoop</span></div>
        <nav aria-label="เมนูหลัก">
          <div className="nav-group">งานประจำวัน</div>
          {WORK.map(([to, label, ready]) => (
            <NavLink key={to} to={to} className={ready ? 'nav-item' : 'nav-item nav-soon'}>
              {label}{!ready && <span className="soon-tag">เร็วๆ นี้</span>}
            </NavLink>
          ))}
          {user.role === 'admin' && (
            <>
              <div className="nav-group">ผู้ดูแลระบบ</div>
              <NavLink to="/users" className="nav-item">ผู้ใช้</NavLink>
              <NavLink to="/audit" className="nav-item">บันทึกการใช้งาน</NavLink>
            </>
          )}
        </nav>
        <div className="me">
          <div className="avatar avatar-lg" aria-hidden="true">{initials}</div>
          <div className="me-text">
            <div className="me-name">{user.name || user.email}</div>
            <div className="me-role">{ROLES[user.role]?.label || user.role}</div>
          </div>
          <button className="btn btn-quiet" onClick={() => logout()}>ออกจากระบบ</button>
        </div>
      </aside>
      <main className="main"><Outlet /></main>
    </div>
  )
}

function Home() {
  const { user } = useAuth()
  return <Navigate to={user?.role === 'admin' ? '/users' : '/overview'} replace />
}

export default function App() {
  const { ready } = useAuth()
  if (!ready) return <div className="boot" aria-busy="true" />
  return (
    <Routes>
      <Route path="/login" element={<Login />} />
      <Route path="/set-password" element={<SetPassword />} />
      <Route path="/change-password" element={<ChangePassword />} />
      <Route element={<Shell />}>
        <Route path="/" element={<Home />} />
        <Route path="/users" element={<Users />} />
        <Route path="/audit" element={<Audit />} />
        <Route path="/campaigns" element={<Campaigns />} />
        <Route path="/campaigns/:id" element={<CampaignDetail />} />
        <Route path="/approvals" element={<Approvals />} />
        <Route path="/overview" element={<Overview />} />
        <Route path="/leads" element={<Leads />} />
        <Route path="/leads/:id" element={<LeadDetail />} />
        <Route path="/scoring" element={<Scoring />} />
        {WORK.filter((w) => !w[2]).map(([to, label]) => (
          <Route key={to} path={to} element={<Soon title={label} />} />
        ))}
      </Route>
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  )
}
