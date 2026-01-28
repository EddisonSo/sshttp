import { useEffect } from 'react'
import { Routes, Route, Navigate } from 'react-router-dom'
import Login from './pages/Login'
import Register from './pages/Register'
import Terminal from './pages/Terminal'
import Settings from './pages/Settings'
import { applyThemeToDocument } from './lib/themes'

function App() {
  useEffect(() => {
    applyThemeToDocument()

    const handleStorageChange = (e: StorageEvent) => {
      if (e.key === 'terminal-active-theme') {
        applyThemeToDocument()
      }
    }

    window.addEventListener('storage', handleStorageChange)
    return () => window.removeEventListener('storage', handleStorageChange)
  }, [])

  return (
    <Routes>
      <Route path="/" element={<Navigate to="/login" replace />} />
      <Route path="/login" element={<Login />} />
      <Route path="/register" element={<Register />} />
      <Route path="/terminal" element={<Terminal />} />
      <Route path="/settings" element={<Settings />} />
    </Routes>
  )
}

export default App
