import { LoopDiagram, LoopMark, ThemeToggle } from '../ui.jsx'
import { useLang } from '../i18n.jsx'

// Two-column frame for login / set password: the loop on the left, the form on the right.
export default function AuthFrame({ title, intro, children }) {
  const { lang, setLang } = useLang()
  return (
    <div className="auth">
      <div className="auth-quick-settings"><div className="seg seg-sm" role="group" aria-label="Language"><button className={lang === 'th' ? 'seg-on' : ''} onClick={() => setLang('th')}>ไทย</button><button className={lang === 'en' ? 'seg-on' : ''} onClick={() => setLang('en')}>EN</button></div><ThemeToggle compact /></div>
      <section className="auth-art" aria-hidden="false">
        <div className="brand brand-big"><LoopMark size={34} /><span>LeadLoop</span></div>
        <p className="auth-tag">{lang === 'en' ? 'From campaign planning to sales-ready leads — and back into the next campaign.' : 'จากแผนแคมเปญ ถึงลีดที่เซลส์พร้อมปิด และกลับมาเป็นข้อมูลให้แผนรอบถัดไป'}</p>
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
