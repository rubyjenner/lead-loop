import { LoopMark, ThemeToggle } from '../ui.jsx'
import { useLang } from '../i18n.jsx'

// Landing-style frame for login / set password: headline and the form on the left, a small product preview on the right.
// The preview uses made-up sample data only.
function Preview({ en }) {
  return (
    <div className="auth2-visual" aria-hidden="true">
      <div className="vcard vcard-lead">
        <div className="vrow">
          <span className="vavatar">S</span>
          <div><div className="vname">{en ? 'Somchai (sample)' : 'คุณสมชาย (ตัวอย่าง)'}</div><div className="vsub">LINE · {en ? 'Gadget Sale campaign' : 'แคมเปญลดล้างสต็อก'}</div></div>
          <span className="vtag vtag-hot">Hot · 82</span>
        </div>
        <div className="vbar"><i style={{ width: '82%' }} /></div>
        <div className="vrow vrow-end"><span className="vtag vtag-ok">{en ? 'Ready to hand off' : 'พร้อมส่งต่อ'}</span><span className="vsub">{en ? 'Asked about price · Budget stated' : 'ถามราคา · ระบุงบ'}</span></div>
      </div>
      <div className="vcard vcard-chat">
        <div className="vbubble vbubble-in">{en ? 'Interested! How much is it?' : 'สนใจค่ะ ราคาเท่าไหร่คะ'}</div>
        <div className="vbubble vbubble-out">{en ? 'It is 11,900 THB and installments are available.' : 'ราคา 11,900 บาท ผ่อนได้ค่ะ'}</div>
        <div className="vsub vsub-spark">✦ {en ? 'AI answers only from product facts' : 'AI ตอบจากข้อมูลสินค้าจริงเท่านั้น'}</div>
      </div>
      <div className="vcard vcard-stats">
        <div className="vsub">{en ? 'Leads this month' : 'ลีดเดือนนี้'}</div>
        <div className="vbig">128</div>
        <div className="vbars"><i style={{ height: '30%' }} /><i style={{ height: '48%' }} /><i style={{ height: '40%' }} /><i style={{ height: '66%' }} /><i style={{ height: '58%' }} /><i style={{ height: '88%' }} /></div>
      </div>
      <div className="vcard vcard-approve">
        <span className="vcheck">✓</span>
        <div><div className="vname">{en ? 'Content approved' : 'คอนเทนต์อนุมัติแล้ว'}</div><div className="vsub">{en ? 'Approved by someone other than the creator' : 'อนุมัติโดยคนละคนกับผู้สร้าง'}</div></div>
      </div>
    </div>
  )
}

export default function AuthFrame({ title, intro, children }) {
  const { lang, setLang } = useLang()
  const en = lang === 'en'
  return (
    <div className="auth2">
      <header className="auth2-nav">
        <div className="brand"><LoopMark size={28} /><span>LeadLoop</span></div>
        <div className="auth2-tools">
          <div className="seg seg-sm" role="group" aria-label="Language"><button className={lang === 'th' ? 'seg-on' : ''} onClick={() => setLang('th')}>ไทย</button><button className={lang === 'en' ? 'seg-on' : ''} onClick={() => setLang('en')}>EN</button></div>
          <ThemeToggle compact />
        </div>
      </header>
      <main className="auth2-hero">
        <section className="auth2-copy">
          <p className="auth2-eyebrow">{en ? 'Marketing & sales, one loop' : 'การตลาดและการขาย ในวงจรเดียว'}</p>
          <h1 className="auth2-title">{en ? <>With LeadLoop, you can <em>close the loop</em></> : <>วางแผน ร่าง อนุมัติ และปิดการขาย <em>ครบในที่เดียว</em></>}</h1>
          <p className="auth2-lead">{en ? 'Plan campaigns, let AI draft content, approve it with a second pair of eyes, and hand ready leads to sales, then feed the results into the next campaign.' : 'วางแผนแคมเปญ ให้ AI ช่วยร่างคอนเทนต์ ผ่านการอนุมัติจากอีกคน แล้วส่งลีดที่พร้อมให้เซลส์ และนำผลลัพธ์กลับไปปรับแผนรอบถัดไป'}</p>
          <div className="auth2-card">
            <h2>{title}</h2>
            {intro && <p className="muted small">{intro}</p>}
            {children}
          </div>
        </section>
        <Preview en={en} />
      </main>
    </div>
  )
}
