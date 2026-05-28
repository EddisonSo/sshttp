import { useEffect, useRef, useState } from 'react'
import { api, type FileEntry } from '../lib/api'

interface FileBrowserProps {
  token: string
  sessionId: string
  onClose: () => void
  onError?: (message: string) => void
}

function parentOf(path: string): string | null {
  if (!path || path === '/') return null
  const cleaned = path.endsWith('/') ? path.slice(0, -1) : path
  const idx = cleaned.lastIndexOf('/')
  if (idx < 0) return null
  if (idx === 0) return '/'
  return cleaned.slice(0, idx)
}

function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
  return `${(bytes / (1024 * 1024 * 1024)).toFixed(2)} GB`
}

export default function FileBrowser({ token, sessionId, onClose, onError }: FileBrowserProps) {
  const [path, setPath] = useState<string>('')
  const [entries, setEntries] = useState<FileEntry[]>([])
  const [loading, setLoading] = useState(true)
  const [errorMsg, setErrorMsg] = useState<string | null>(null)
  const containerRef = useRef<HTMLDivElement>(null)

  const loadPath = async (newPath: string) => {
    setLoading(true)
    setErrorMsg(null)
    try {
      const res = await api.listFiles(token, sessionId, newPath || undefined)
      setPath(res.path)
      setEntries(res.entries ?? [])
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to list files'
      setErrorMsg(message)
      onError?.(message)
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    loadPath('')
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [sessionId])

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])

  const handleEntryClick = (entry: FileEntry) => {
    if (entry.isDir) {
      const next = path === '/' ? `/${entry.name}` : `${path}/${entry.name}`
      loadPath(next)
      return
    }
    const fullPath = path === '/' ? `/${entry.name}` : `${path}/${entry.name}`
    const url = api.getDownloadUrl(token, sessionId, fullPath)
    const a = document.createElement('a')
    a.href = url
    a.download = entry.name
    a.rel = 'noopener'
    document.body.appendChild(a)
    a.click()
    document.body.removeChild(a)
  }

  const goUp = () => {
    const parent = parentOf(path)
    if (parent) loadPath(parent)
  }

  return (
    <div
      className="fixed inset-0 z-50 flex items-start justify-end bg-black/30"
      onClick={(e) => {
        if (e.target === e.currentTarget) onClose()
      }}
    >
      <div
        ref={containerRef}
        className="mt-14 mr-4 flex h-[70vh] w-[28rem] flex-col rounded-lg border border-[var(--theme-border)] bg-[var(--theme-bg-secondary)] shadow-2xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center gap-2 border-b border-[var(--theme-border)] px-3 py-2">
          <button
            onClick={goUp}
            disabled={!parentOf(path)}
            className="rounded p-1 text-[var(--theme-fg-muted)] transition hover:bg-[var(--theme-bg)] hover:text-[var(--theme-fg)] disabled:cursor-not-allowed disabled:opacity-30"
            title="Go up"
          >
            <svg className="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2}>
              <path strokeLinecap="round" strokeLinejoin="round" d="M10.5 19.5 3 12m0 0 7.5-7.5M3 12h18" />
            </svg>
          </button>
          <div className="flex-1 truncate font-mono text-xs text-[var(--theme-fg)]" title={path}>
            {path || '...'}
          </div>
          <button
            onClick={onClose}
            className="rounded p-1 text-[var(--theme-fg-muted)] transition hover:bg-[var(--theme-bg)] hover:text-[var(--theme-fg)]"
            title="Close"
          >
            <svg className="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2}>
              <path strokeLinecap="round" strokeLinejoin="round" d="M6 18L18 6M6 6l12 12" />
            </svg>
          </button>
        </div>

        <div className="flex-1 overflow-y-auto">
          {loading ? (
            <div className="p-6 text-center text-sm text-[var(--theme-fg-muted)]">Loading...</div>
          ) : errorMsg ? (
            <div className="p-6 text-center text-sm text-red-400">{errorMsg}</div>
          ) : entries.length === 0 ? (
            <div className="p-6 text-center text-sm text-[var(--theme-fg-muted)]">Empty directory</div>
          ) : (
            <ul className="py-1">
              {entries.map((entry) => (
                <li key={entry.name}>
                  <button
                    onClick={() => handleEntryClick(entry)}
                    className="flex w-full items-center gap-2 px-3 py-1.5 text-left text-sm text-[var(--theme-fg)] transition hover:bg-[var(--theme-bg)]"
                    title={entry.isDir ? 'Open folder' : 'Download file'}
                  >
                    {entry.isDir ? (
                      <svg className="h-4 w-4 flex-shrink-0 text-blue-400" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={1.5}>
                        <path strokeLinecap="round" strokeLinejoin="round" d="M2.25 12.75V12A2.25 2.25 0 0 1 4.5 9.75h15A2.25 2.25 0 0 1 21.75 12v.75m-8.69-6.44-2.12-2.12a1.5 1.5 0 0 0-1.061-.44H4.5A2.25 2.25 0 0 0 2.25 6v12a2.25 2.25 0 0 0 2.25 2.25h15A2.25 2.25 0 0 0 21.75 18V9a2.25 2.25 0 0 0-2.25-2.25h-5.379a1.5 1.5 0 0 1-1.06-.44Z" />
                      </svg>
                    ) : (
                      <svg className="h-4 w-4 flex-shrink-0 text-[var(--theme-fg-muted)]" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={1.5}>
                        <path strokeLinecap="round" strokeLinejoin="round" d="M19.5 14.25v-2.625a3.375 3.375 0 0 0-3.375-3.375h-1.5A1.125 1.125 0 0 1 13.5 7.125v-1.5a3.375 3.375 0 0 0-3.375-3.375H8.25m2.25 0H5.625c-.621 0-1.125.504-1.125 1.125v17.25c0 .621.504 1.125 1.125 1.125h12.75c.621 0 1.125-.504 1.125-1.125V11.25a9 9 0 0 0-9-9Z" />
                      </svg>
                    )}
                    <span className="flex-1 truncate">{entry.name}</span>
                    {!entry.isDir && (
                      <span className="ml-2 flex-shrink-0 text-xs text-[var(--theme-fg-muted)]">
                        {formatSize(entry.size)}
                      </span>
                    )}
                  </button>
                </li>
              ))}
            </ul>
          )}
        </div>
      </div>
    </div>
  )
}
