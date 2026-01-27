import { useEffect, useState, useCallback } from 'react'
import { useNavigate } from 'react-router-dom'
import XTerm from '../components/XTerm'
import { useTerminal } from '../hooks/useTerminal'
import { api } from '../lib/api'

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
  onError,
  onExit,
  onFileComplete,
  onFileError,
}: {
  token: string
  sessionId?: string
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
      <XTerm ref={termRef} onData={handleData} onResize={handleResize} onFileDrop={handleFileDrop} />
      {fileUpload && (
        <div className="absolute bottom-4 right-4 rounded-lg bg-gray-800 px-4 py-3 shadow-lg">
          <div className="mb-1 text-sm text-gray-300">
            Uploading: {fileUpload.filename}
          </div>
          <div className="h-2 w-48 overflow-hidden rounded-full bg-gray-700">
            <div
              className="h-full bg-blue-500 transition-all duration-150"
              style={{
                width: `${fileUpload.totalBytes > 0 ? (fileUpload.bytesUploaded / fileUpload.totalBytes) * 100 : 0}%`,
              }}
            />
          </div>
          <div className="mt-1 text-xs text-gray-400">
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
  }, [navigate])

  const loadSessions = async (t: string) => {
    try {
      const res = await api.listSessions(t)
      if (res.sessions.length > 0) {
        const existingTabs = res.sessions.map(s => ({
          id: s.id,
          name: s.name,
        }))
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

  if (!token || loading) {
    return (
      <div className="flex h-screen items-center justify-center">
        <div className="text-gray-400">Loading...</div>
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
      <div className="flex items-center border-b border-gray-700 bg-gray-900">
        {/* Tabs */}
        <div className="flex flex-1 items-center overflow-x-auto">
          {tabs.map(tab => (
            <div
              key={tab.id}
              className={`group flex items-center justify-between gap-2 border-r border-gray-700 px-4 py-2 cursor-pointer transition-all ${
                activeTab === tab.id
                  ? 'bg-gray-800 text-white'
                  : 'text-gray-400 hover:bg-gray-800/50 hover:text-white'
              }`}
              style={{
                minWidth: tabs.length <= 3 ? `${Math.floor(200 / tabs.length)}px` : '80px',
                maxWidth: tabs.length <= 3 ? '200px' : '150px',
                flex: tabs.length <= 5 ? '1' : '0 0 auto',
              }}
              onClick={() => setActiveTab(tab.id)}
            >
              {editingTab === tab.id ? (
                <input
                  type="text"
                  value={editName}
                  onChange={(e) => setEditName(e.target.value)}
                  onBlur={finishRename}
                  onKeyDown={handleRenameKeyDown}
                  className="w-24 bg-gray-700 px-1 text-sm text-white outline-none"
                  autoFocus
                  onClick={(e) => e.stopPropagation()}
                />
              ) : (
                <span
                  className="text-sm"
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
                className="opacity-0 group-hover:opacity-100 text-gray-500 hover:text-white"
              >
                x
              </button>
            </div>
          ))}
          <button
            onClick={() => createNewTab()}
            className="px-3 py-2 text-gray-400 hover:bg-gray-800/50 hover:text-white"
            title="New tab"
          >
            +
          </button>
        </div>

        {/* Actions */}
        <div className="flex gap-2 px-4">
          <button
            onClick={() => navigate('/settings')}
            className="rounded px-3 py-1 text-sm text-gray-400 transition hover:bg-gray-800 hover:text-white"
          >
            Settings
          </button>
          <button
            onClick={handleLogout}
            className="rounded px-3 py-1 text-sm text-gray-400 transition hover:bg-gray-800 hover:text-white"
          >
            Logout
          </button>
        </div>
      </div>

      {/* Terminal content */}
      <div className="flex-1 overflow-hidden p-2">
        {showCreatePrompt ? (
          <div className="flex h-full items-center justify-center">
            <div className="w-full max-w-md rounded-lg border border-gray-700 bg-gray-800 p-6">
              <h2 className="mb-4 text-lg font-semibold">Create New Session</h2>
              <input
                type="text"
                value={newSessionName}
                onChange={(e) => setNewSessionName(e.target.value)}
                onKeyDown={handleCreateKeyDown}
                placeholder="Session name (optional)"
                className="mb-4 w-full rounded bg-gray-700 px-3 py-2 text-white placeholder-gray-400 outline-none focus:ring-2 focus:ring-blue-500"
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
