import { createContext, useCallback, useContext, useEffect, useRef, useState } from 'react'
import { EN_MAP } from './i18n-en.js'

// Thai / English switch for the whole app.
// 1) t(key, thaiText) is used for a few labels that need an explicit key.
// 2) Everything else is translated at the screen level: when English is on, a DOM translator replaces Thai text
//    (text nodes, placeholder/title/aria-label/alt, alert/confirm dialogs) using the phrase table in i18n-en.js.
//    New Thai text only needs a new row in i18n-en.js; untranslated text simply stays Thai.
const KEY = 'leadloop-lang'
const EN = {
  'nav.daily': 'Daily work', 'nav.admin': 'Admin', 'nav.campaigns': 'Campaigns & content', 'nav.leads': 'Customers & leads', 'nav.insights': 'Insights & settings',
  '/overview': 'Overview', '/campaigns': 'Campaigns', '/approvals': 'Content approval', '/leads': 'Leads',
  '/followups': 'Follow-ups', '/scoring': 'Scoring rules', '/reports': 'Reports', '/chat-test': 'Customer chat test',
  '/users': 'Users', '/retention': 'Data retention', '/audit': 'Activity log', '/integrations': 'Integrations',
  'me.logout': 'Log out', 'me.profile': 'Profile & settings',
  'theme.dark': 'Dark mode', 'theme.light': 'Light mode',
  'profile.title': 'Profile & settings', 'profile.sub': 'Your name and position are shown instead of your e-mail on campaigns and approvals.',
  'profile.email': 'E-mail', 'profile.role': 'Role', 'profile.name': 'Full name', 'profile.job': 'Position',
  'profile.save': 'Save', 'profile.saved': 'Saved', 'profile.password': 'Change password',
  'profile.appearance': 'Appearance', 'profile.theme': 'Theme', 'profile.lang': 'Language',
  'profile.light': 'Light', 'profile.dark': 'Dark',
}

// ---------- screen-level translator ----------
const TH = /[฀-๿]/
const esc = (x) => x.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
const exact = new Map(Object.entries(EN_MAP))
const fragments = Object.keys(EN_MAP).filter((k) => k.length >= 6).sort((a, b) => b.length - a.length)
const fragRe = fragments.length ? new RegExp(fragments.map(esc).join('|'), 'g') : null
const MONTHS = { 'ม.ค.': 'Jan', 'ก.พ.': 'Feb', 'มี.ค.': 'Mar', 'เม.ย.': 'Apr', 'พ.ค.': 'May', 'มิ.ย.': 'Jun', 'ก.ค.': 'Jul', 'ส.ค.': 'Aug', 'ก.ย.': 'Sep', 'ต.ค.': 'Oct', 'พ.ย.': 'Nov', 'ธ.ค.': 'Dec' }
const monthRe = new RegExp(`(\\d{1,2})\\s*(${Object.keys(MONTHS).map(esc).join('|')})(\\s*(25\\d\\d))?`, 'g')

export function translate(text) {
  if (typeof text !== 'string' || !TH.test(text)) return text
  const lead = text.match(/^\s*/)[0]
  const trail = text.match(/\s*$/)[0]
  const core = text.trim().replace(/\s+/g, ' ')
  const hit = exact.get(core)
  if (hit !== undefined) return lead + hit + trail
  let out = core
  // Thai dates (Buddhist year) -> "15 Nov 2026", numeric 15/11/2569 -> 15/11/2026
  out = out.replace(monthRe, (_m, d, mo, _y, y) => `${d} ${MONTHS[mo]}${y ? ` ${Number(y) - 543}` : ''}`)
  out = out.replace(/(\d{1,2})\/(\d{1,2})\/(25\d\d)/g, (_m, d, mo, y) => `${d}/${mo}/${Number(y) - 543}`)
  if (fragRe) out = out.replace(fragRe, (m) => exact.get(m) ?? m)
  out = out.replace(/บาท/g, 'THB')
  return lead + out + trail
}

const ATTRS = ['placeholder', 'title', 'aria-label', 'alt']
const textOrig = new WeakMap() // text node -> { th, en }
const attrOrig = new WeakMap() // element -> { attr: { th, en } }
const SKIP = new Set(['SCRIPT', 'STYLE', 'TEXTAREA', 'CODE'])

function applyText(node, lang) {
  const cur = node.nodeValue
  const rec = textOrig.get(node)
  if (rec && cur === rec.en) {
    if (lang === 'th') { node.nodeValue = rec.th; textOrig.delete(node) }
    return
  }
  if (rec) textOrig.delete(node) // React wrote a new value
  if (lang !== 'en' || !TH.test(cur)) return
  const en = translate(cur)
  if (en !== cur) { textOrig.set(node, { th: cur, en }); node.nodeValue = en }
}
function applyAttrs(el, lang) {
  let rec = attrOrig.get(el)
  for (const a of ATTRS) {
    if (!el.hasAttribute || !el.hasAttribute(a)) continue
    const cur = el.getAttribute(a)
    const r = rec?.[a]
    if (r && cur === r.en) { if (lang === 'th') { el.setAttribute(a, r.th); delete rec[a] } continue }
    if (r) delete rec[a]
    if (lang !== 'en' || !TH.test(cur)) continue
    const en = translate(cur)
    if (en !== cur) { rec = rec || {}; rec[a] = { th: cur, en }; attrOrig.set(el, rec); el.setAttribute(a, en) }
  }
}
function applyTree(root, lang) {
  if (!root) return
  if (root.nodeType === Node.TEXT_NODE) { if (!SKIP.has(root.parentNode?.nodeName)) applyText(root, lang); return }
  if (root.nodeType !== Node.ELEMENT_NODE || SKIP.has(root.nodeName)) return
  applyAttrs(root, lang)
  const walker = document.createTreeWalker(root, NodeFilter.SHOW_ELEMENT | NodeFilter.SHOW_TEXT)
  let n = walker.nextNode()
  while (n) {
    if (n.nodeType === Node.TEXT_NODE) { if (!SKIP.has(n.parentNode?.nodeName)) applyText(n, lang) }
    else if (SKIP.has(n.nodeName)) { n = walker.nextSibling() || walker.nextNode(); continue } else applyAttrs(n, lang)
    n = walker.nextNode()
  }
}
let docTitleTh = null
function applyTitle(lang) {
  if (lang === 'en') { if (TH.test(document.title)) { docTitleTh = document.title; document.title = translate(document.title) } }
  else if (docTitleTh) { document.title = docTitleTh; docTitleTh = null }
}

const Ctx = createContext({ lang: 'th', setLang: () => {}, t: (k, th) => th ?? k })
export const useLang = () => useContext(Ctx)

export function LangProvider({ children }) {
  const [lang, set] = useState(() => { try { return localStorage.getItem(KEY) === 'en' ? 'en' : 'th' } catch { return 'th' } })
  const setLang = useCallback((l) => { set(l); try { localStorage.setItem(KEY, l) } catch { /* ignore */ } document.documentElement.lang = l }, [])
  useEffect(() => { document.documentElement.lang = lang }, [lang])
  // t(key, thaiText): Thai text is passed at the call site, English comes from the table above
  const t = useCallback((k, th) => (lang === 'en' ? (EN[k] ?? th ?? k) : (th ?? k)), [lang])
  const langRef = useRef(lang)
  useEffect(() => {
    langRef.current = lang
    applyTree(document.body, lang)
    applyTitle(lang)
  }, [lang])
  useEffect(() => {
    // keep the screen translated while React updates it
    const obs = new MutationObserver((records) => {
      const l = langRef.current
      for (const r of records) {
        if (r.type === 'characterData') applyTree(r.target, l)
        else if (r.type === 'attributes') applyAttrs(r.target, l)
        else r.addedNodes.forEach((n) => applyTree(n, l))
      }
    })
    obs.observe(document.body, { subtree: true, childList: true, characterData: true, attributes: true, attributeFilter: ATTRS })
    // dialogs written in Thai in the code are translated too
    const orig = { alert: window.alert, confirm: window.confirm, prompt: window.prompt }
    window.alert = (m) => orig.alert.call(window, langRef.current === 'en' ? translate(String(m)) : m)
    window.confirm = (m) => orig.confirm.call(window, langRef.current === 'en' ? translate(String(m)) : m)
    window.prompt = (m, d) => orig.prompt.call(window, langRef.current === 'en' ? translate(String(m)) : m, d)
    return () => { obs.disconnect(); Object.assign(window, orig) }
  }, [])
  return <Ctx.Provider value={{ lang, setLang, t }}>{children}</Ctx.Provider>
}
