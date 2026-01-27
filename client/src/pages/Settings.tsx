import { useState, useEffect } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, ApiError, KeyInfo } from '../lib/api'
import {
  isWebAuthnSupported,
  parseCreationOptions,
  createCredential,
  serializeCreationResponse,
} from '../lib/webauthn'

export default function Settings() {
  const navigate = useNavigate()
  const [keys, setKeys] = useState<KeyInfo[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [actionLoading, setActionLoading] = useState<string | null>(null)
  const [deleteConfirm, setDeleteConfirm] = useState<string | null>(null)
  const [editingKey, setEditingKey] = useState<string | null>(null)
  const [editName, setEditName] = useState('')

  const token = sessionStorage.getItem('accessToken')

  useEffect(() => {
    if (!token) {
      navigate('/login')
      return
    }
    loadKeys()
  }, [token, navigate])

  const loadKeys = async () => {
    if (!token) return
    try {
      const res = await api.listKeys(token)
      setKeys(res.keys)
      setError('')
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        sessionStorage.removeItem('accessToken')
        navigate('/login')
      } else {
        setError('Failed to load keys')
      }
    } finally {
      setLoading(false)
    }
  }

  const handleDelete = async (id: string) => {
    if (!token) return

    setDeleteConfirm(null)
    setActionLoading(id)
    try {
      await api.deleteKey(token, id)
      await loadKeys()
    } catch (err) {
      if (err instanceof ApiError) {
        setError(err.message)
      } else {
        setError('Failed to delete key')
      }
    } finally {
      setActionLoading(null)
    }
  }

  const startRename = (key: KeyInfo) => {
    setEditingKey(key.id)
    setEditName(key.name || key.authenticatorType)
  }

  const finishRename = async () => {
    if (!token || !editingKey || !editName.trim()) {
      setEditingKey(null)
      return
    }

    setActionLoading(editingKey)
    try {
      await api.renameKey(token, editingKey, editName.trim())
      await loadKeys()
    } catch (err) {
      if (err instanceof ApiError) {
        setError(err.message)
      } else {
        setError('Failed to rename key')
      }
    } finally {
      setActionLoading(null)
      setEditingKey(null)
    }
  }

  const handleRenameKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter') {
      finishRename()
    } else if (e.key === 'Escape') {
      setEditingKey(null)
    }
  }

  const getKeyDisplayName = (key: KeyInfo) => {
    if (key.name) return key.name
    return key.authenticatorType
  }

  const handleAddKey = async () => {
    if (!token) return

    setActionLoading('add')
    setError('')

    try {
      // Step 1: Begin add key
      const beginRes = await api.addKeyBegin(token)

      // Step 2: Create credential with authenticator
      const options = parseCreationOptions(beginRes.options as unknown as Record<string, unknown>)
      const credential = await createCredential(options)

      // Step 3: Finish add key
      const serialized = serializeCreationResponse(credential)
      await api.addKeyFinish(token, {
        state: beginRes.state,
        credential: serialized,
      })

      await loadKeys()
    } catch (err) {
      if (err instanceof ApiError) {
        setError(err.message)
      } else if (err instanceof Error) {
        if (err.name === 'NotAllowedError') {
          setError('Operation was cancelled or timed out')
        } else {
          setError(err.message)
        }
      } else {
        setError('Failed to add key')
      }
    } finally {
      setActionLoading(null)
    }
  }

  const formatDate = (dateStr: string) => {
    return new Date(dateStr).toLocaleDateString(undefined, {
      year: 'numeric',
      month: 'short',
      day: 'numeric',
      hour: '2-digit',
      minute: '2-digit',
    })
  }

  if (!token) return null

  return (
    <div className="flex min-h-screen flex-col">
      {/* Header */}
      <div className="flex items-center justify-between border-b border-gray-700 bg-gray-900 px-4 py-2">
        <h1 className="text-lg font-semibold">Settings</h1>
        <div className="flex gap-2">
          <button
            onClick={() => navigate('/terminal')}
            className="rounded px-3 py-1 text-sm text-gray-400 transition hover:bg-gray-800 hover:text-white"
          >
            Terminal
          </button>
          <button
            onClick={() => {
              sessionStorage.removeItem('accessToken')
              navigate('/login')
            }}
            className="rounded px-3 py-1 text-sm text-gray-400 transition hover:bg-gray-800 hover:text-white"
          >
            Logout
          </button>
        </div>
      </div>

      {/* Content */}
      <div className="flex-1 p-6">
        <div className="mx-auto max-w-2xl">
          <h2 className="mb-6 text-2xl font-bold">Passkeys</h2>

          {error && (
            <div className="mb-4 rounded-lg bg-red-900/50 p-3 text-sm text-red-400">{error}</div>
          )}

          {loading ? (
            <div className="text-gray-400">Loading...</div>
          ) : (
            <>
              <div className="mb-6 space-y-3">
                {keys.map((key) => (
                  <div
                    key={key.id}
                    className="flex items-center justify-between rounded-lg border border-gray-700 bg-gray-800 p-4"
                  >
                    <div className="flex-1">
                      {editingKey === key.id ? (
                        <input
                          type="text"
                          value={editName}
                          onChange={(e) => setEditName(e.target.value)}
                          onBlur={finishRename}
                          onKeyDown={handleRenameKeyDown}
                          className="w-48 rounded bg-gray-700 px-2 py-1 text-white outline-none focus:ring-2 focus:ring-blue-500"
                          autoFocus
                        />
                      ) : (
                        <div
                          className="cursor-pointer font-medium hover:text-blue-400"
                          onDoubleClick={() => startRename(key)}
                          title="Double-click to rename"
                        >
                          {getKeyDisplayName(key)}
                        </div>
                      )}
                      <div className="text-sm text-gray-400">
                        {key.authenticatorType} &middot; Added {formatDate(key.createdAt)}
                      </div>
                    </div>
                    <button
                      onClick={() => setDeleteConfirm(key.id)}
                      disabled={actionLoading === key.id || keys.length === 1}
                      className="rounded px-3 py-1 text-sm text-red-400 transition hover:bg-red-900/50 disabled:cursor-not-allowed disabled:opacity-50"
                      title={keys.length === 1 ? 'Cannot delete last passkey' : 'Delete passkey'}
                    >
                      {actionLoading === key.id ? '...' : 'Delete'}
                    </button>
                  </div>
                ))}
              </div>

              {/* Delete confirmation modal */}
              {deleteConfirm && (
                <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50">
                  <div className="mx-4 w-full max-w-sm rounded-lg border border-gray-700 bg-gray-800 p-6">
                    <h3 className="mb-4 text-lg font-semibold">Delete Passkey</h3>
                    <p className="mb-6 text-gray-400">
                      Are you sure you want to delete this passkey? This action cannot be undone.
                    </p>
                    <div className="flex justify-end gap-3">
                      <button
                        onClick={() => setDeleteConfirm(null)}
                        className="rounded px-4 py-2 text-gray-400 transition hover:bg-gray-700 hover:text-white"
                      >
                        Cancel
                      </button>
                      <button
                        onClick={() => handleDelete(deleteConfirm)}
                        disabled={actionLoading === deleteConfirm}
                        className="rounded bg-red-600 px-4 py-2 font-medium text-white transition hover:bg-red-700 disabled:opacity-50"
                      >
                        {actionLoading === deleteConfirm ? 'Deleting...' : 'Delete'}
                      </button>
                    </div>
                  </div>
                </div>
              )}

              {isWebAuthnSupported() && (
                <button
                  onClick={handleAddKey}
                  disabled={actionLoading === 'add'}
                  className="rounded-lg bg-blue-600 px-4 py-2 font-medium text-white transition hover:bg-blue-700 disabled:cursor-not-allowed disabled:opacity-50"
                >
                  {actionLoading === 'add' ? 'Adding...' : 'Add New Passkey'}
                </button>
              )}
            </>
          )}
        </div>
      </div>
    </div>
  )
}
