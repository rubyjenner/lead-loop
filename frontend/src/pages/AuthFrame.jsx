import { LoopDiagram, LoopMark } from '../ui.jsx'

// Two-column frame for login / set password: the loop on the left, the form on the right.
export default function AuthFrame({ title, intro, children }) {
  return (
    <div className="auth">
      <section className="auth-art" aria-hidden="false">
        <div className="brand brand-big"><LoopMark size={34} /><span>LeadLoop</span></div>
        <p className="auth-tag">จากแผนแคมเปญ ถึงลีดที่เซลส์พร้อมปิด และกลับมาเป็นข้อมูลให้แผนรอบถัดไป</p>
        <LoopDiagram />
      </section>
      <section className="auth-form">
        <div className="auth-card">
          <h1>{title}</h1>
          {intro && <p className="muted">{intro}</p>}
          {children}
        </div>
      </section>
    </div>
  )
}
