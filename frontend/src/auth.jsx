import { createContext, useCallback, useContext, useEffect, useState } from 'react'
import { api, getToken, setToken, setUnauthorizedHandler } from './api.js'

const Ctx = createContext(null)
export const useAuth = () => useContext(Ctx)

export function AuthProvider({ children }) {
  const [user, setUser] = useState(null) // {email, name, role}
  const [ready, setReady] = useState(false)
  const [notice, setNotice] = useState('')

  const logout = useCallback((msg) => {
    setToken('')
    setUser(null)
    if (msg) setNotice(msg)
  }, [])

  useEffect(() => {
    setUnauthorizedHandler(() => logout('เซสชันหมดอายุ ล็อกอินอีกครั้ง'))
    if (!getToken()) { setReady(true); return }
    api('GET', '/auth/me')
      .then((me) => setUser(me))
      .catch(() => {})
      .finally(() => setReady(true))
  }, [logout])

  async function login(email, password) {
    const r = await api('POST', '/auth/login', { email, password })
    setToken(r.token)
    setNotice('')
    const me = await api('GET', '/auth/me').catch(() => ({ email, role: r.role, name: '' }))
    setUser({ ...me, mustChange: r.must_change_password })
    return r
  }

  return <Ctx.Provider value={{ user, setUser, ready, login, logout, notice, setNotice }}>{children}</Ctx.Provider>
}
