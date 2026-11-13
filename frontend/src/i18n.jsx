import { createContext, useCallback, useContext, useEffect, useState } from 'react'

// Thai / English switch. Covers the menu, the top bar and the profile page for now;
// the other pages are still Thai only (add keys here as pages get translated).
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
  'profile.light': 'Light', 'profile.dark': 'Dark', 'profile.note': 'Only the menu and this page are translated so far.',
}
const Ctx = createContext({ lang: 'th', setLang: () => {}, t: (k, th) => th ?? k })
export const useLang = () => useContext(Ctx)

export function LangProvider({ children }) {
  const [lang, set] = useState(() => { try { return localStorage.getItem(KEY) === 'en' ? 'en' : 'th' } catch { return 'th' } })
  const setLang = useCallback((l) => { set(l); try { localStorage.setItem(KEY, l) } catch { /* ignore */ } document.documentElement.lang = l }, [])
  useEffect(() => { document.documentElement.lang = lang }, [lang])
  // t(key, thaiText): Thai text is passed at the call site, English comes from the table above
  const t = useCallback((k, th) => (lang === 'en' ? (EN[k] ?? th ?? k) : (th ?? k)), [lang])
  return <Ctx.Provider value={{ lang, setLang, t }}>{children}</Ctx.Provider>
}
