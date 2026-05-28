import { useEffect, useState } from 'react'
import { Routes, Route, Navigate } from 'react-router-dom'
import Login from './pages/Login'
import Register from './pages/Register'
import Terminal from './pages/Terminal'
import Settings from './pages/Settings'
import { applyThemeToDocument } from './lib/themes'
import { api } from './lib/api'

// Sentinel token used in no-auth mode so the existing token guards pass.
// The server ignores tokens entirely when auth is disabled.
const NOAUTH_TOKEN = 'noauth'

function App() {
  const [authEnabled, setAuthEnabled] = useState<boolean | null>(null)

  useEffect(() => {
    document.title = window.location.hostname
    applyThemeToDocument()

    const handleStorageChange = (e: StorageEvent) => {
      if (e.key === 'terminal-active-theme') {
        applyThemeToDocument()
      }
    }

    window.addEventListener('storage', handleStorageChange)
    return () => window.removeEventListener('storage', handleStorageChange)
  }, [])

  useEffect(() => {
    api.getConfig()
      .then(cfg => {
        if (!cfg.authEnabled && !sessionStorage.getItem('accessToken')) {
          sessionStorage.setItem('accessToken', NOAUTH_TOKEN)
        }
        setAuthEnabled(cfg.authEnabled)
      })
      .catch(() => setAuthEnabled(true)) // fail safe: assume auth is on
  }, [])

  if (authEnabled === null) return null

  return (
    <Routes>
      <Route path="/" element={<Navigate to={authEnabled ? '/login' : '/terminal'} replace />} />
      <Route path="/login" element={authEnabled ? <Login /> : <Navigate to="/terminal" replace />} />
      <Route path="/register" element={authEnabled ? <Register /> : <Navigate to="/terminal" replace />} />
      <Route path="/terminal" element={<Terminal />} />
      <Route path="/settings" element={<Settings />} />
    </Routes>
  )
}

export default App
