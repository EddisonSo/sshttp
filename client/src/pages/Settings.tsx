import { useState, useEffect, useRef } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, ApiError, KeyInfo } from '../lib/api'
import {
  isWebAuthnSupported,
  parseCreationOptions,
  createCredential,
  serializeCreationResponse,
} from '../lib/webauthn'
import { TerminalTheme } from '../lib/itermThemeParser'
import {
  getCachedThemes,
  getActiveThemeName,
  loadThemesFromServer,
  saveThemeToServer,
  deleteThemeFromServer,
  setActiveThemeOnServer,
} from '../lib/themes'
import {
  getCachedFonts,
  getActiveFontName,
  getFontSize,
  setFontSize,
  loadFontsFromServer,
  uploadFontToServer,
  deleteFontFromServer,
  setActiveFontOnServer,
  isFontAvailable,
  preloadBuiltinFonts,
  BUILTIN_FONTS,
  CustomFont,
  MIN_FONT_SIZE,
  MAX_FONT_SIZE,
} from '../lib/fonts'

export default function Settings() {
  const navigate = useNavigate()
  const [keys, setKeys] = useState<KeyInfo[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [actionLoading, setActionLoading] = useState<string | null>(null)
  const [deleteConfirm, setDeleteConfirm] = useState<string | null>(null)
  const [editingKey, setEditingKey] = useState<string | null>(null)
  const [editName, setEditName] = useState('')

  // Theme state
  const [themes, setThemes] = useState<TerminalTheme[]>(() => getCachedThemes())
  const [activeTheme, setActiveThemeState] = useState(() => getActiveThemeName())
  const [themeInput, setThemeInput] = useState('')
  const [themeName, setThemeName] = useState('')
  const [themeError, setThemeError] = useState('')
  const [themeSaving, setThemeSaving] = useState(false)
  const [deleteThemeConfirm, setDeleteThemeConfirm] = useState<string | null>(null)

  // Font state
  const [fonts, setFonts] = useState<CustomFont[]>(() => getCachedFonts())
  const [activeFontName, setActiveFontState] = useState(() => getActiveFontName())
  const [currentFontSize, setCurrentFontSize] = useState(() => getFontSize())
  const [fontError, setFontError] = useState('')
  const [fontUploading, setFontUploading] = useState(false)
  const [deleteFontConfirm, setDeleteFontConfirm] = useState<string | null>(null)
  const [fontsReady, setFontsReady] = useState(false)
  const fontInputRef = useRef<HTMLInputElement>(null)

  // Preload fonts to ensure availability checks are accurate
  useEffect(() => {
    preloadBuiltinFonts().then(() => setFontsReady(true))
  }, [])

  const token = sessionStorage.getItem('accessToken')

  useEffect(() => {
    if (!token) {
      navigate('/login')
      return
    }
    loadKeys()
    loadCustomization()
  }, [token, navigate])

  const loadCustomization = async () => {
    if (!token) return
    try {
      // Load themes from server
      const themesData = await loadThemesFromServer(token)
      setThemes(themesData.themes)
      setActiveThemeState(themesData.activeTheme)

      // Load fonts from server
      const fontsData = await loadFontsFromServer(token)
      setFonts(fontsData.fonts)
      setActiveFontState(fontsData.activeFont)
    } catch (err) {
      console.error('Failed to load customization:', err)
    }
  }

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

  const handleImportTheme = async () => {
    if (!token) return
    if (!themeInput.trim()) {
      setThemeError('Please paste the theme XML content')
      return
    }
    if (!themeName.trim()) {
      setThemeError('Please enter a theme name')
      return
    }

    try {
      setThemeSaving(true)
      setThemeError('')
      const theme = await saveThemeToServer(token, themeName.trim(), themeInput.trim())
      setThemes(prev => [...prev.filter(t => t.name !== theme.name), theme])
      setThemeInput('')
      setThemeName('')
    } catch (err) {
      setThemeError(err instanceof Error ? err.message : 'Failed to save theme')
    } finally {
      setThemeSaving(false)
    }
  }

  const handleSelectTheme = async (name: string) => {
    if (!token) return
    try {
      await setActiveThemeOnServer(token, name)
      setActiveThemeState(name)
    } catch (err) {
      console.error('Failed to set active theme:', err)
    }
  }

  const handleDeleteTheme = async (name: string) => {
    if (!token) return
    try {
      await deleteThemeFromServer(token, name)
      setThemes(prev => prev.filter(t => t.name !== name))
      if (activeTheme === name) {
        setActiveThemeState('Default')
      }
    } catch (err) {
      console.error('Failed to delete theme:', err)
    }
    setDeleteThemeConfirm(null)
  }

  const handleFontUpload = async (e: React.ChangeEvent<HTMLInputElement>) => {
    if (!token) return
    const file = e.target.files?.[0]
    if (!file) return

    try {
      setFontUploading(true)
      setFontError('')
      const font = await uploadFontToServer(token, file)
      setFonts(prev => [...prev.filter(f => f.name !== font.name), font])
      if (fontInputRef.current) {
        fontInputRef.current.value = ''
      }
    } catch (err) {
      setFontError(err instanceof Error ? err.message : 'Failed to upload font')
    } finally {
      setFontUploading(false)
    }
  }

  const handleSelectFont = async (name: string) => {
    if (!token) return
    try {
      await setActiveFontOnServer(token, name)
      setActiveFontState(name)
    } catch (err) {
      console.error('Failed to set active font:', err)
    }
  }

  const handleDeleteFont = async (name: string) => {
    if (!token) return
    try {
      await deleteFontFromServer(token, name)
      setFonts(prev => prev.filter(f => f.name !== name))
      if (activeFontName === name) {
        setActiveFontState('monospace')
      }
    } catch (err) {
      console.error('Failed to delete font:', err)
    }
    setDeleteFontConfirm(null)
  }

  const handleFontSizeChange = (delta: number) => {
    const newSize = currentFontSize + delta
    if (newSize >= MIN_FONT_SIZE && newSize <= MAX_FONT_SIZE) {
      setCurrentFontSize(newSize)
      setFontSize(newSize)
    }
  }

  if (!token) return null

  return (
    <div className="flex min-h-screen flex-col">
      {/* Header */}
      <div className="flex min-h-[44px] items-center justify-end bg-[var(--theme-bg-secondary)] px-2 pt-2">
        <div className="flex gap-1">
          <button
            onClick={() => navigate('/terminal')}
            className="rounded-lg p-2 text-[var(--theme-fg-muted)] transition hover:bg-[var(--theme-bg-tertiary)] hover:text-[var(--theme-fg)]"
            title="Terminal"
          >
            <svg className="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={1.5}>
              <path strokeLinecap="round" strokeLinejoin="round" d="m6.75 7.5 3 2.25-3 2.25m4.5 0h3m-9 8.25h13.5A2.25 2.25 0 0 0 21 18V6a2.25 2.25 0 0 0-2.25-2.25H5.25A2.25 2.25 0 0 0 3 6v12a2.25 2.25 0 0 0 2.25 2.25Z" />
            </svg>
          </button>
          <button
            onClick={() => {
              sessionStorage.removeItem('accessToken')
              navigate('/login')
            }}
            className="rounded-lg p-2 text-[var(--theme-fg-muted)] transition hover:bg-[var(--theme-bg-tertiary)] hover:text-[var(--theme-fg)]"
            title="Logout"
          >
            <svg className="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={1.5}>
              <path strokeLinecap="round" strokeLinejoin="round" d="M15.75 9V5.25A2.25 2.25 0 0 0 13.5 3h-6a2.25 2.25 0 0 0-2.25 2.25v13.5A2.25 2.25 0 0 0 7.5 21h6a2.25 2.25 0 0 0 2.25-2.25V15m3 0 3-3m0 0-3-3m3 3H9" />
            </svg>
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
            <div className="text-[var(--theme-fg-muted)]">Loading...</div>
          ) : (
            <>
              <div className="mb-6 space-y-3">
                {keys.map((key) => (
                  <div
                    key={key.id}
                    className="flex items-center justify-between rounded-lg border border-[var(--theme-border)] bg-[var(--theme-bg-secondary)] p-4"
                  >
                    <div className="flex-1">
                      {editingKey === key.id ? (
                        <input
                          type="text"
                          value={editName}
                          onChange={(e) => setEditName(e.target.value)}
                          onBlur={finishRename}
                          onKeyDown={handleRenameKeyDown}
                          className="w-48 rounded bg-[var(--theme-bg-tertiary)] px-2 py-1 text-[var(--theme-fg)] outline-none focus:ring-2 focus:ring-blue-500"
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
                      <div className="text-sm text-[var(--theme-fg-muted)]">
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
                  <div className="mx-4 w-full max-w-sm rounded-lg border border-[var(--theme-border)] bg-[var(--theme-bg-secondary)] p-6">
                    <h3 className="mb-4 text-lg font-semibold">Delete Passkey</h3>
                    <p className="mb-6 text-[var(--theme-fg-muted)]">
                      Are you sure you want to delete this passkey? This action cannot be undone.
                    </p>
                    <div className="flex justify-end gap-3">
                      <button
                        onClick={() => setDeleteConfirm(null)}
                        className="rounded px-4 py-2 text-[var(--theme-fg-muted)] transition hover:bg-[var(--theme-bg-tertiary)] hover:text-[var(--theme-fg)]"
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

          {/* Themes Section */}
          <h2 className="mb-6 mt-12 text-2xl font-bold">Terminal Themes</h2>

          {/* Theme list */}
          <div className="mb-6 space-y-2">
            {themes.map((theme) => (
              <div
                key={theme.name}
                className={`flex items-center justify-between rounded-lg border p-3 cursor-pointer transition ${
                  activeTheme === theme.name
                    ? 'border-blue-500 bg-blue-900/20'
                    : 'border-[var(--theme-border)] bg-[var(--theme-bg-secondary)] hover:border-[var(--theme-fg-muted)]'
                }`}
                onClick={() => handleSelectTheme(theme.name)}
              >
                <div className="flex items-center gap-3">
                  {activeTheme === theme.name && (
                    <span className="text-blue-400">&#10003;</span>
                  )}
                  <span className={activeTheme === theme.name ? 'font-medium' : ''}>
                    {theme.name}
                  </span>
                  <div className="flex gap-1">
                    <div
                      className="h-4 w-4 rounded border border-[var(--theme-border)]"
                      style={{ backgroundColor: theme.background }}
                      title="Background"
                    />
                    <div
                      className="h-4 w-4 rounded border border-[var(--theme-border)]"
                      style={{ backgroundColor: theme.foreground }}
                      title="Foreground"
                    />
                    <div
                      className="h-4 w-4 rounded border border-[var(--theme-border)]"
                      style={{ backgroundColor: theme.red }}
                      title="Red"
                    />
                    <div
                      className="h-4 w-4 rounded border border-[var(--theme-border)]"
                      style={{ backgroundColor: theme.green }}
                      title="Green"
                    />
                    <div
                      className="h-4 w-4 rounded border border-[var(--theme-border)]"
                      style={{ backgroundColor: theme.blue }}
                      title="Blue"
                    />
                  </div>
                </div>
                {theme.name !== 'Default' && (
                  <button
                    onClick={(e) => {
                      e.stopPropagation()
                      setDeleteThemeConfirm(theme.name)
                    }}
                    className="rounded px-2 py-1 text-sm text-red-400 transition hover:bg-red-900/50"
                  >
                    Delete
                  </button>
                )}
              </div>
            ))}
          </div>

          {/* Delete theme confirmation modal */}
          {deleteThemeConfirm && (
            <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50">
              <div className="mx-4 w-full max-w-sm rounded-lg border border-[var(--theme-border)] bg-[var(--theme-bg-secondary)] p-6">
                <h3 className="mb-4 text-lg font-semibold">Delete Theme</h3>
                <p className="mb-6 text-[var(--theme-fg-muted)]">
                  Are you sure you want to delete "{deleteThemeConfirm}"?
                </p>
                <div className="flex justify-end gap-3">
                  <button
                    onClick={() => setDeleteThemeConfirm(null)}
                    className="rounded px-4 py-2 text-[var(--theme-fg-muted)] transition hover:bg-[var(--theme-bg-tertiary)] hover:text-[var(--theme-fg)]"
                  >
                    Cancel
                  </button>
                  <button
                    onClick={() => handleDeleteTheme(deleteThemeConfirm)}
                    className="rounded bg-red-600 px-4 py-2 font-medium text-white transition hover:bg-red-700"
                  >
                    Delete
                  </button>
                </div>
              </div>
            </div>
          )}

          {/* Import theme */}
          <div className="rounded-lg border border-[var(--theme-border)] bg-[var(--theme-bg-secondary)] p-4">
            <h3 className="mb-3 font-medium">Import iTerm2 Theme</h3>
            <p className="mb-3 text-sm text-[var(--theme-fg-muted)]">
              Paste the contents of an .itermcolors file below. You can find themes at{' '}
              <a
                href="https://iterm2colorschemes.com"
                target="_blank"
                rel="noopener noreferrer"
                className="text-blue-400 hover:underline"
              >
                iterm2colorschemes.com
              </a>
            </p>
            {themeError && (
              <div className="mb-3 rounded bg-red-900/50 p-2 text-sm text-red-400">
                {themeError}
              </div>
            )}
            <input
              type="text"
              value={themeName}
              onChange={(e) => setThemeName(e.target.value)}
              placeholder="Theme name"
              className="mb-3 w-full rounded bg-[var(--theme-bg-tertiary)] px-3 py-2 text-[var(--theme-fg)] placeholder-[var(--theme-fg-muted)] outline-none focus:ring-2 focus:ring-blue-500"
            />
            <textarea
              value={themeInput}
              onChange={(e) => setThemeInput(e.target.value)}
              placeholder="Paste .itermcolors XML content here..."
              className="mb-3 h-32 w-full resize-none rounded bg-[var(--theme-bg-tertiary)] px-3 py-2 font-mono text-sm text-[var(--theme-fg)] placeholder-[var(--theme-fg-muted)] outline-none focus:ring-2 focus:ring-blue-500"
            />
            <button
              onClick={handleImportTheme}
              disabled={themeSaving}
              className="rounded bg-blue-600 px-4 py-2 font-medium text-white transition hover:bg-blue-700 disabled:opacity-50 disabled:cursor-not-allowed"
            >
              {themeSaving ? 'Importing...' : 'Import Theme'}
            </button>
          </div>

          {/* Fonts Section */}
          <h2 className="mb-6 mt-12 text-2xl font-bold">Terminal Font</h2>

          {/* Font size control */}
          <div className="mb-6 flex items-center gap-4 rounded-lg border border-[var(--theme-border)] bg-[var(--theme-bg-secondary)] p-4">
            <span className="font-medium">Font Size</span>
            <div className="flex items-center gap-2">
              <button
                onClick={() => handleFontSizeChange(-1)}
                disabled={currentFontSize <= MIN_FONT_SIZE}
                className="rounded bg-[var(--theme-bg-tertiary)] px-3 py-1 text-lg font-bold transition hover:bg-[var(--theme-fg-muted)]/20 disabled:cursor-not-allowed disabled:opacity-50"
              >
                -
              </button>
              <span className="w-12 text-center text-lg font-mono">{currentFontSize}px</span>
              <button
                onClick={() => handleFontSizeChange(1)}
                disabled={currentFontSize >= MAX_FONT_SIZE}
                className="rounded bg-[var(--theme-bg-tertiary)] px-3 py-1 text-lg font-bold transition hover:bg-[var(--theme-fg-muted)]/20 disabled:cursor-not-allowed disabled:opacity-50"
              >
                +
              </button>
            </div>
          </div>

          {/* Font list */}
          <div className="mb-6 space-y-2">
            {/* Built-in fonts */}
            {BUILTIN_FONTS.map((fontName) => {
              // Only check availability after fonts have been preloaded
              const isAvailable = !fontsReady || fontName === 'monospace' || isFontAvailable(fontName)
              return (
                <div
                  key={fontName}
                  className={`flex items-center justify-between rounded-lg border p-3 cursor-pointer transition ${
                    activeFontName === fontName
                      ? 'border-blue-500 bg-blue-900/20'
                      : 'border-[var(--theme-border)] bg-[var(--theme-bg-secondary)] hover:border-[var(--theme-fg-muted)]'
                  }`}
                  onClick={() => handleSelectFont(fontName)}
                >
                  <div className="flex items-center gap-3">
                    {activeFontName === fontName && (
                      <span className="text-blue-400">&#10003;</span>
                    )}
                    <div>
                      <div className="flex items-center gap-2">
                        <span className={activeFontName === fontName ? 'font-medium' : ''}>
                          {fontName}
                        </span>
                        {!isAvailable && (
                          <span className="text-xs text-yellow-500" title="Font not installed on system">
                            (not available)
                          </span>
                        )}
                      </div>
                      <div
                        className="text-sm text-[var(--theme-fg-muted)]"
                        style={{ fontFamily: isAvailable ? `"${fontName}"` : 'monospace' }}
                      >
                        AaBbCc 0123456789 {"->"}!=
                      </div>
                    </div>
                  </div>
                </div>
              )
            })}

            {/* Custom fonts */}
            {fonts.map((font) => {
              const isAvailable = isFontAvailable(font.name)
              // Only show error if font is genuinely not available (ignore stale error messages)
              const hasError = !isAvailable
              return (
                <div
                  key={font.name}
                  className={`flex items-center justify-between rounded-lg border p-3 cursor-pointer transition ${
                    activeFontName === font.name
                      ? 'border-blue-500 bg-blue-900/20'
                      : hasError
                        ? 'border-red-500/50 bg-red-900/10 hover:border-red-500'
                        : 'border-[var(--theme-border)] bg-[var(--theme-bg-secondary)] hover:border-[var(--theme-fg-muted)]'
                  }`}
                  onClick={() => handleSelectFont(font.name)}
                >
                  <div className="flex items-center gap-3">
                    {activeFontName === font.name && (
                      <span className="text-blue-400">&#10003;</span>
                    )}
                    <div>
                      <div className="flex items-center gap-2">
                        <span className={activeFontName === font.name ? 'font-medium' : ''}>
                          {font.name}
                        </span>
                        {hasError && (
                          <span className="text-xs text-red-400" title={font.error || 'Font failed to load'}>
                            (failed to load)
                          </span>
                        )}
                      </div>
                      <div
                        className="text-sm text-[var(--theme-fg-muted)]"
                        style={{ fontFamily: isAvailable ? `"${font.name}"` : 'monospace' }}
                      >
                        AaBbCc 0123456789 {"->"}!=
                      </div>
                    </div>
                  </div>
                  <button
                    onClick={(e) => {
                      e.stopPropagation()
                      setDeleteFontConfirm(font.name)
                    }}
                    className="rounded px-2 py-1 text-sm text-red-400 transition hover:bg-red-900/50"
                  >
                    Delete
                  </button>
                </div>
              )
            })}
          </div>

          {/* Delete font confirmation modal */}
          {deleteFontConfirm && (
            <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50">
              <div className="mx-4 w-full max-w-sm rounded-lg border border-[var(--theme-border)] bg-[var(--theme-bg-secondary)] p-6">
                <h3 className="mb-4 text-lg font-semibold">Delete Font</h3>
                <p className="mb-6 text-[var(--theme-fg-muted)]">
                  Are you sure you want to delete "{deleteFontConfirm}"?
                </p>
                <div className="flex justify-end gap-3">
                  <button
                    onClick={() => setDeleteFontConfirm(null)}
                    className="rounded px-4 py-2 text-[var(--theme-fg-muted)] transition hover:bg-[var(--theme-bg-tertiary)] hover:text-[var(--theme-fg)]"
                  >
                    Cancel
                  </button>
                  <button
                    onClick={() => handleDeleteFont(deleteFontConfirm)}
                    className="rounded bg-red-600 px-4 py-2 font-medium text-white transition hover:bg-red-700"
                  >
                    Delete
                  </button>
                </div>
              </div>
            </div>
          )}

          {/* Upload font */}
          <div className="rounded-lg border border-[var(--theme-border)] bg-[var(--theme-bg-secondary)] p-4">
            <h3 className="mb-3 font-medium">Upload Custom Font</h3>
            <p className="mb-3 text-sm text-[var(--theme-fg-muted)]">
              Upload a .ttf, .otf, .woff, or .woff2 font file. Monospace fonts work best for terminals.
            </p>
            {fontError && (
              <div className="mb-3 rounded bg-red-900/50 p-2 text-sm text-red-400">
                {fontError}
              </div>
            )}
            <input
              ref={fontInputRef}
              type="file"
              accept=".ttf,.otf,.woff,.woff2"
              onChange={handleFontUpload}
              disabled={fontUploading}
              className="block w-full text-sm text-[var(--theme-fg-muted)] file:mr-4 file:rounded file:border-0 file:bg-blue-600 file:px-4 file:py-2 file:text-sm file:font-medium file:text-white hover:file:bg-blue-700 disabled:opacity-50 disabled:cursor-not-allowed"
            />
            {fontUploading && (
              <p className="mt-2 text-sm text-[var(--theme-fg-muted)]">Uploading font...</p>
            )}
          </div>
        </div>
      </div>
    </div>
  )
}
