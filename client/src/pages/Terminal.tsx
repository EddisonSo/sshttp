import { useEffect, useState, useCallback } from 'react'
import { useNavigate } from 'react-router-dom'
import XTerm from '../components/XTerm'
import { useTerminal } from '../hooks/useTerminal'
import { api } from '../lib/api'
import { getActiveTheme, loadThemesFromServer } from '../lib/themes'
import { getActiveFontName, getFontFamily, getFontSize, loadFontsFromServer } from '../lib/fonts'
import type { TerminalTheme } from '../lib/itermThemeParser'

interface Tab {
  id: string
  name: string
}

interface Toast {
  id: number
  type: 'success' | 'error'
  message: string
}

function TerminalTab({
  token,
  sessionId,
  theme,
  fontFamily,
  fontSize,
  isActive,
  onError,
  onExit,
  onFileComplete,
  onFileError,
}: {
  token: string
  sessionId?: string
  theme?: TerminalTheme
  fontFamily?: string
  fontSize?: number
  isActive?: boolean
  onError: () => void
  onExit: (code: number) => void
  onFileComplete?: (filename: string) => void
  onFileError?: (error: string) => void
}) {
  const { termRef, handleData, handleResize, handleFileDrop, fileUpload } = useTerminal({
    token,
    sessionId,
    onExit,
    onError,
    onFileComplete,
    onFileError,
  })

  return (
    <div className="relative h-full w-full">
      <XTerm ref={termRef} onData={handleData} onResize={handleResize} onFileDrop={handleFileDrop} theme={theme} fontFamily={fontFamily} fontSize={fontSize} isActive={isActive} />
      {fileUpload && (
        <div className="absolute bottom-4 right-4 rounded-lg bg-[var(--theme-bg-secondary)] px-4 py-3 shadow-lg">
          <div className="mb-1 text-sm text-[var(--theme-fg)]">
            Uploading: {fileUpload.filename}
          </div>
          <div className="h-2 w-48 overflow-hidden rounded-full bg-[var(--theme-bg-tertiary)]">
            <div
              className="h-full bg-blue-500 transition-all duration-150"
              style={{
                width: `${fileUpload.totalBytes > 0 ? (fileUpload.bytesUploaded / fileUpload.totalBytes) * 100 : 0}%`,
              }}
            />
          </div>
          <div className="mt-1 text-xs text-[var(--theme-fg-muted)]">
            {Math.round(fileUpload.bytesUploaded / 1024)} / {Math.round(fileUpload.totalBytes / 1024)} KB
          </div>
        </div>
      )}
    </div>
  )
}

let toastIdCounter = 0

export default function Terminal() {
  const navigate = useNavigate()
  const [token, setToken] = useState<string | null>(null)
  const [tabs, setTabs] = useState<Tab[]>([])
  const [activeTab, setActiveTab] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)
  const [editingTab, setEditingTab] = useState<string | null>(null)
  const [editName, setEditName] = useState('')
  const [newSessionName, setNewSessionName] = useState('')
  const [showCreatePrompt, setShowCreatePrompt] = useState(false)
  const [toasts, setToasts] = useState<Toast[]>([])
  const [theme, setTheme] = useState<TerminalTheme>(() => getActiveTheme())
  const [fontFamily, setFontFamily] = useState<string>(() => getFontFamily(getActiveFontName()))
  const [fontSize, setFontSize] = useState<number>(() => getFontSize())
  const [draggedTab, setDraggedTab] = useState<string | null>(null)
  const [dragOverTab, setDragOverTab] = useState<string | null>(null)

  const addToast = useCallback((type: 'success' | 'error', message: string) => {
    const id = ++toastIdCounter
    setToasts(prev => [...prev, { id, type, message }])
    const duration = type === 'success' ? 3000 : 5000
    setTimeout(() => {
      setToasts(prev => prev.filter(t => t.id !== id))
    }, duration)
  }, [])

  const handleFileComplete = useCallback((filename: string) => {
    addToast('success', `Uploaded: ${filename}`)
  }, [addToast])

  const handleFileError = useCallback((error: string) => {
    addToast('error', `Upload failed: ${error}`)
  }, [addToast])

  useEffect(() => {
    const storedToken = sessionStorage.getItem('accessToken')
    if (!storedToken) {
      navigate('/login')
      return
    }
    setToken(storedToken)
    loadSessions(storedToken)
    loadCustomization(storedToken)
  }, [navigate])

  const loadCustomization = async (t: string) => {
    try {
      // Load themes and fonts from server
      const [, fontsData] = await Promise.all([
        loadThemesFromServer(t),
        loadFontsFromServer(t),
      ])

      // Update active theme/font from server
      setTheme(getActiveTheme())
      setFontFamily(getFontFamily(fontsData.activeFont || getActiveFontName()))
    } catch (err) {
      console.error('Failed to load customization:', err)
    }
  }

  useEffect(() => {
    const handleStorageChange = (e: StorageEvent) => {
      if (e.key === 'terminal-active-theme') {
        setTheme(getActiveTheme())
      }
      if (e.key === 'terminal-active-font') {
        setFontFamily(getFontFamily(getActiveFontName()))
      }
      if (e.key === 'terminal-font-size') {
        setFontSize(getFontSize())
      }
    }
    window.addEventListener('storage', handleStorageChange)
    return () => window.removeEventListener('storage', handleStorageChange)
  }, [])

  const loadSessions = async (t: string) => {
    try {
      const res = await api.listSessions(t)
      if (res.sessions.length > 0) {
        const existingTabs = res.sessions.map(s => ({
          id: s.id,
          name: s.name,
        }))
        // Restore saved tab order if available
        const savedOrder = localStorage.getItem('tab-order')
        if (savedOrder) {
          try {
            const order = JSON.parse(savedOrder) as string[]
            existingTabs.sort((a, b) => {
              const aIndex = order.indexOf(a.id)
              const bIndex = order.indexOf(b.id)
              // Tabs not in saved order go to the end
              if (aIndex === -1 && bIndex === -1) return 0
              if (aIndex === -1) return 1
              if (bIndex === -1) return -1
              return aIndex - bIndex
            })
          } catch {
            // Invalid saved order, use default
          }
        }
        setTabs(existingTabs)
        setActiveTab(existingTabs[0].id)
        setShowCreatePrompt(false)
      } else {
        // Show create prompt instead of auto-creating
        setShowCreatePrompt(true)
      }
    } catch (err) {
      // If unauthorized, redirect to login
      sessionStorage.removeItem('accessToken')
      navigate('/login')
    } finally {
      setLoading(false)
    }
  }

  const createNewTab = async (t?: string, name?: string) => {
    const tokenToUse = t || token
    if (!tokenToUse) return

    try {
      const res = await api.createSession(tokenToUse, name || undefined)
      const newTab = { id: res.id, name: res.name }
      setTabs(prev => [...prev, newTab])
      setActiveTab(res.id)
      setShowCreatePrompt(false)
      setNewSessionName('')
    } catch (err) {
      console.error('Failed to create session', err)
    }
  }

  const handleCreateSession = () => {
    createNewTab(undefined, newSessionName.trim() || undefined)
  }

  const handleCreateKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter') {
      handleCreateSession()
    }
  }

  const closeTab = async (tabId: string) => {
    if (!token) return

    try {
      // Delete the session on the server
      await api.deleteSession(token, tabId)
    } catch (err) {
      console.error('Failed to delete session', err)
    }

    setTabs(prev => {
      const filtered = prev.filter(t => t.id !== tabId)
      if (activeTab === tabId && filtered.length > 0) {
        setActiveTab(filtered[0].id)
      }
      if (filtered.length === 0) {
        setShowCreatePrompt(true)
      }
      return filtered
    })
  }

  const handleLogout = () => {
    sessionStorage.removeItem('accessToken')
    navigate('/login')
  }

  const handleError = () => {
    sessionStorage.removeItem('accessToken')
    navigate('/login')
  }

  const handleExit = (tabId: string) => () => {
    // Remove the tab when shell exits
    closeTab(tabId)
  }

  const startRename = (tab: Tab) => {
    setEditingTab(tab.id)
    setEditName(tab.name)
  }

  const finishRename = async () => {
    if (!token || !editingTab || !editName.trim()) {
      setEditingTab(null)
      return
    }

    try {
      await api.renameSession(token, editingTab, editName.trim())
      setTabs(prev => prev.map(t =>
        t.id === editingTab ? { ...t, name: editName.trim() } : t
      ))
    } catch (err) {
      console.error('Failed to rename session', err)
    }
    setEditingTab(null)
  }

  const handleRenameKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter') {
      finishRename()
    } else if (e.key === 'Escape') {
      setEditingTab(null)
    }
  }

  const handleDragStart = (e: React.DragEvent, tabId: string) => {
    setDraggedTab(tabId)
    e.dataTransfer.effectAllowed = 'move'
    e.dataTransfer.setData('text/plain', tabId)
  }

  const handleDragOver = (e: React.DragEvent, tabId: string) => {
    e.preventDefault()
    if (draggedTab && draggedTab !== tabId) {
      setDragOverTab(tabId)
    }
  }

  const handleDragLeave = () => {
    setDragOverTab(null)
  }

  const saveTabOrder = (newTabs: Tab[]) => {
    const order = newTabs.map(t => t.id)
    localStorage.setItem('tab-order', JSON.stringify(order))
  }

  const handleDrop = (e: React.DragEvent, targetTabId: string) => {
    e.preventDefault()
    if (!draggedTab || draggedTab === targetTabId) {
      setDraggedTab(null)
      setDragOverTab(null)
      return
    }

    setTabs(prev => {
      const newTabs = [...prev]
      const draggedIndex = newTabs.findIndex(t => t.id === draggedTab)
      const targetIndex = newTabs.findIndex(t => t.id === targetTabId)
      const [removed] = newTabs.splice(draggedIndex, 1)
      newTabs.splice(targetIndex, 0, removed)
      saveTabOrder(newTabs)
      return newTabs
    })

    setDraggedTab(null)
    setDragOverTab(null)
  }

  // Drop at start of tab bar (leftmost position)
  const handleDropAtStart = (e: React.DragEvent) => {
    e.preventDefault()
    if (!draggedTab) {
      setDragOverTab(null)
      return
    }

    setTabs(prev => {
      const newTabs = [...prev]
      const draggedIndex = newTabs.findIndex(t => t.id === draggedTab)
      if (draggedIndex === 0) {
        // Already at start
        return prev
      }
      const [removed] = newTabs.splice(draggedIndex, 1)
      newTabs.unshift(removed)
      saveTabOrder(newTabs)
      return newTabs
    })

    setDraggedTab(null)
    setDragOverTab(null)
  }

  // Drop at end of tab bar (rightmost position)
  const handleDropAtEnd = (e: React.DragEvent) => {
    e.preventDefault()
    if (!draggedTab) {
      setDragOverTab(null)
      return
    }

    setTabs(prev => {
      const newTabs = [...prev]
      const draggedIndex = newTabs.findIndex(t => t.id === draggedTab)
      if (draggedIndex === newTabs.length - 1) {
        // Already at end
        return prev
      }
      const [removed] = newTabs.splice(draggedIndex, 1)
      newTabs.push(removed)
      saveTabOrder(newTabs)
      return newTabs
    })

    setDraggedTab(null)
    setDragOverTab(null)
  }

  const handleDragEnd = () => {
    setDraggedTab(null)
    setDragOverTab(null)
  }

  if (!token || loading) {
    return (
      <div className="flex h-screen items-center justify-center">
        <div className="text-[var(--theme-fg-muted)]">Loading...</div>
      </div>
    )
  }

  return (
    <div className="flex h-screen flex-col">
      {/* Toast notifications */}
      <div className="fixed top-4 right-4 z-50 flex flex-col gap-2">
        {toasts.map(toast => (
          <div
            key={toast.id}
            className={`rounded-lg px-4 py-3 shadow-lg transition-all duration-300 ${
              toast.type === 'success'
                ? 'bg-green-600 text-white'
                : 'bg-red-600 text-white'
            }`}
          >
            {toast.message}
          </div>
        ))}
      </div>

      {/* Header with tabs */}
      <div className="flex items-center bg-[var(--theme-bg-secondary)]">
        {/* Tabs */}
        <div className="flex flex-1 items-center gap-1 overflow-x-auto px-2 pt-2">
          {/* Drop zone for dragging to start */}
          {draggedTab && (
            <div
              className={`flex-shrink-0 w-8 h-8 rounded transition-all ${
                dragOverTab === 'start' ? 'bg-blue-500/30 ring-2 ring-blue-500' : ''
              }`}
              onDragOver={(e) => {
                e.preventDefault()
                setDragOverTab('start')
              }}
              onDragLeave={handleDragLeave}
              onDrop={handleDropAtStart}
            />
          )}
          {tabs.map(tab => (
            <div
              key={tab.id}
              draggable={editingTab !== tab.id}
              onDragStart={(e) => handleDragStart(e, tab.id)}
              onDragOver={(e) => handleDragOver(e, tab.id)}
              onDragLeave={handleDragLeave}
              onDrop={(e) => handleDrop(e, tab.id)}
              onDragEnd={handleDragEnd}
              className={`group flex items-center gap-2 rounded-t-lg px-4 py-2 cursor-pointer transition-all ${
                activeTab === tab.id
                  ? 'bg-[var(--theme-bg)] text-[var(--theme-fg)]'
                  : 'text-[var(--theme-fg-muted)] hover:bg-[var(--theme-bg)]/50 hover:text-[var(--theme-fg)]'
              } ${
                draggedTab === tab.id ? 'opacity-50' : ''
              } ${
                dragOverTab === tab.id ? 'ring-2 ring-blue-500 ring-inset' : ''
              }`}
              style={{
                minWidth: tabs.length <= 3 ? '150px' : '100px',
                maxWidth: tabs.length <= 3 ? '250px' : '180px',
                flex: tabs.length <= 5 ? '1 1 auto' : '0 0 auto',
              }}
              onClick={() => setActiveTab(tab.id)}
            >
              <svg className="h-4 w-4 flex-shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={1.5}>
                <path strokeLinecap="round" strokeLinejoin="round" d="m6.75 7.5 3 2.25-3 2.25m4.5 0h3m-9 8.25h13.5A2.25 2.25 0 0 0 21 18V6a2.25 2.25 0 0 0-2.25-2.25H5.25A2.25 2.25 0 0 0 3 6v12a2.25 2.25 0 0 0 2.25 2.25Z" />
              </svg>
              {editingTab === tab.id ? (
                <input
                  type="text"
                  value={editName}
                  onChange={(e) => setEditName(e.target.value)}
                  onBlur={finishRename}
                  onKeyDown={handleRenameKeyDown}
                  className="w-full min-w-0 flex-1 bg-transparent px-1 text-sm text-[var(--theme-fg)] outline-none"
                  autoFocus
                  onClick={(e) => e.stopPropagation()}
                />
              ) : (
                <span
                  className="flex-1 truncate text-sm"
                  onDoubleClick={(e) => {
                    e.stopPropagation()
                    startRename(tab)
                  }}
                >
                  {tab.name}
                </span>
              )}
              <button
                onClick={(e) => {
                  e.stopPropagation()
                  closeTab(tab.id)
                }}
                className="flex-shrink-0 rounded p-0.5 opacity-0 transition-opacity group-hover:opacity-100 hover:bg-[var(--theme-bg-tertiary)] text-[var(--theme-fg-muted)] hover:text-[var(--theme-fg)]"
              >
                <svg className="h-3.5 w-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2}>
                  <path strokeLinecap="round" strokeLinejoin="round" d="M6 18L18 6M6 6l12 12" />
                </svg>
              </button>
            </div>
          ))}
          {/* Drop zone for dragging to end */}
          {draggedTab && (
            <div
              className={`flex-shrink-0 w-8 h-8 rounded transition-all ${
                dragOverTab === 'end' ? 'bg-blue-500/30 ring-2 ring-blue-500' : ''
              }`}
              onDragOver={(e) => {
                e.preventDefault()
                setDragOverTab('end')
              }}
              onDragLeave={handleDragLeave}
              onDrop={handleDropAtEnd}
            />
          )}
          <button
            onClick={() => createNewTab()}
            className="flex-shrink-0 rounded-lg p-2 text-[var(--theme-fg-muted)] transition hover:bg-[var(--theme-bg)]/50 hover:text-[var(--theme-fg)]"
            title="New tab"
          >
            <svg className="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2}>
              <path strokeLinecap="round" strokeLinejoin="round" d="M12 4v16m8-8H4" />
            </svg>
          </button>
        </div>

        {/* Actions */}
        <div className="flex items-center gap-1 px-2 pt-2">
          <button
            onClick={() => navigate('/settings')}
            className="rounded-lg p-2 text-[var(--theme-fg-muted)] transition hover:bg-[var(--theme-bg)]/50 hover:text-[var(--theme-fg)]"
            title="Settings"
          >
            <svg className="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={1.5}>
              <path strokeLinecap="round" strokeLinejoin="round" d="M9.594 3.94c.09-.542.56-.94 1.11-.94h2.593c.55 0 1.02.398 1.11.94l.213 1.281c.063.374.313.686.645.87.074.04.147.083.22.127.325.196.72.257 1.075.124l1.217-.456a1.125 1.125 0 0 1 1.37.49l1.296 2.247a1.125 1.125 0 0 1-.26 1.431l-1.003.827c-.293.241-.438.613-.43.992a7.723 7.723 0 0 1 0 .255c-.008.378.137.75.43.991l1.004.827c.424.35.534.955.26 1.43l-1.298 2.247a1.125 1.125 0 0 1-1.369.491l-1.217-.456c-.355-.133-.75-.072-1.076.124a6.47 6.47 0 0 1-.22.128c-.331.183-.581.495-.644.869l-.213 1.281c-.09.543-.56.94-1.11.94h-2.594c-.55 0-1.019-.398-1.11-.94l-.213-1.281c-.062-.374-.312-.686-.644-.87a6.52 6.52 0 0 1-.22-.127c-.325-.196-.72-.257-1.076-.124l-1.217.456a1.125 1.125 0 0 1-1.369-.49l-1.297-2.247a1.125 1.125 0 0 1 .26-1.431l1.004-.827c.292-.24.437-.613.43-.991a6.932 6.932 0 0 1 0-.255c.007-.38-.138-.751-.43-.992l-1.004-.827a1.125 1.125 0 0 1-.26-1.43l1.297-2.247a1.125 1.125 0 0 1 1.37-.491l1.216.456c.356.133.751.072 1.076-.124.072-.044.146-.086.22-.128.332-.183.582-.495.644-.869l.214-1.28Z" />
              <path strokeLinecap="round" strokeLinejoin="round" d="M15 12a3 3 0 1 1-6 0 3 3 0 0 1 6 0Z" />
            </svg>
          </button>
          <button
            onClick={handleLogout}
            className="rounded-lg p-2 text-[var(--theme-fg-muted)] transition hover:bg-[var(--theme-bg)]/50 hover:text-[var(--theme-fg)]"
            title="Logout"
          >
            <svg className="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={1.5}>
              <path strokeLinecap="round" strokeLinejoin="round" d="M15.75 9V5.25A2.25 2.25 0 0 0 13.5 3h-6a2.25 2.25 0 0 0-2.25 2.25v13.5A2.25 2.25 0 0 0 7.5 21h6a2.25 2.25 0 0 0 2.25-2.25V15m3 0 3-3m0 0-3-3m3 3H9" />
            </svg>
          </button>
        </div>
      </div>

      {/* Terminal content */}
      <div className="flex-1 overflow-hidden p-2">
        {showCreatePrompt ? (
          <div className="flex h-full items-center justify-center">
            <div className="w-full max-w-md rounded-lg border border-[var(--theme-border)] bg-[var(--theme-bg-secondary)] p-6">
              <h2 className="mb-4 text-lg font-semibold">Create New Session</h2>
              <input
                type="text"
                value={newSessionName}
                onChange={(e) => setNewSessionName(e.target.value)}
                onKeyDown={handleCreateKeyDown}
                placeholder="Session name (optional)"
                className="mb-4 w-full rounded bg-[var(--theme-bg-tertiary)] px-3 py-2 text-[var(--theme-fg)] placeholder-[var(--theme-fg-muted)] outline-none focus:ring-2 focus:ring-blue-500"
                autoFocus
              />
              <button
                onClick={handleCreateSession}
                className="w-full rounded bg-blue-600 px-4 py-2 font-medium text-white transition hover:bg-blue-700"
              >
                Create Session
              </button>
            </div>
          </div>
        ) : (
          tabs.map(tab => (
            <div
              key={tab.id}
              className={`h-full ${activeTab === tab.id ? 'block' : 'hidden'}`}
            >
              <TerminalTab
                token={token}
                sessionId={tab.id}
                theme={theme}
                fontFamily={fontFamily}
                fontSize={fontSize}
                isActive={activeTab === tab.id}
                onError={handleError}
                onExit={handleExit(tab.id)}
                onFileComplete={handleFileComplete}
                onFileError={handleFileError}
              />
            </div>
          ))
        )}
      </div>
    </div>
  )
}
