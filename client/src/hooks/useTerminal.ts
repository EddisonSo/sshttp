import { useEffect, useLayoutEffect, useRef, useCallback, useState } from 'react'
import { connectShell, ShellConnection, FileTransferCallbacks } from '../lib/ws'
import type { XTermHandle } from '../components/XTerm'

interface UseTerminalOptions {
  token: string
  sessionId?: string
  isActive?: boolean
  onExit?: (code: number) => void
  onError?: (error: Error) => void
  onFileProgress?: (bytesUploaded: number, totalBytes: number) => void
  onFileComplete?: (filename: string) => void
  onFileError?: (error: string) => void
  onSessionsChange?: () => void
}

interface FileUploadState {
  uploading: boolean
  filename: string
  bytesUploaded: number
  totalBytes: number
}

export function useTerminal({ token, sessionId, isActive = true, onExit, onError, onFileProgress, onFileComplete, onFileError, onSessionsChange }: UseTerminalOptions) {
  const termRef = useRef<XTermHandle>(null)
  const connRef = useRef<ShellConnection | null>(null)
  const pendingDataRef = useRef<string[]>([]) // Buffer data until terminal is ready
  const [connected, setConnected] = useState(false)
  const [exitCode, setExitCode] = useState<number | null>(null)
  const [fileUpload, setFileUpload] = useState<FileUploadState | null>(null)
  const [isWriter, setIsWriter] = useState(true) // Assume writer until told otherwise
  const [clientCount, setClientCount] = useState(1)

  // Store callbacks in refs to avoid reconnection on callback changes
  const onExitRef = useRef(onExit)
  const onErrorRef = useRef(onError)
  const onFileProgressRef = useRef(onFileProgress)
  const onFileCompleteRef = useRef(onFileComplete)
  const onFileErrorRef = useRef(onFileError)
  const onSessionsChangeRef = useRef(onSessionsChange)
  onExitRef.current = onExit
  onErrorRef.current = onError
  onFileProgressRef.current = onFileProgress
  onFileCompleteRef.current = onFileComplete
  onFileErrorRef.current = onFileError
  onSessionsChangeRef.current = onSessionsChange

  // Connect to shell
  useEffect(() => {
    if (!token) return

    const conn = connectShell(token, {
      onData: (data) => {
        console.log('[useTerminal] onData:', data.length, 'bytes, termRef ready:', !!termRef.current, 'pending:', pendingDataRef.current.length)
        if (termRef.current) {
          // Flush any pending data first
          if (pendingDataRef.current.length > 0) {
            console.log('[useTerminal] Flushing', pendingDataRef.current.length, 'pending chunks')
            for (const pending of pendingDataRef.current) {
              termRef.current.write(pending)
            }
            pendingDataRef.current = []
          }
          termRef.current.write(data)
        } else {
          // Buffer data until terminal is ready
          console.log('[useTerminal] Buffering data, terminal not ready')
          pendingDataRef.current.push(data)
        }
      },
      onExit: (code) => {
        setExitCode(code)
        setConnected(false)
        onExitRef.current?.(code)
      },
      onError: (err) => {
        setConnected(false)
        onErrorRef.current?.(err)
      },
      onClose: () => {
        setConnected(false)
      },
      onOpen: () => {
        // Send initial size once terminal is ready and visible
        // Hidden tabs (display:hidden) can't measure dimensions properly,
        // so we skip the resize here and let the isActive useEffect handle it
        const sendSize = (retries = 5) => {
          const size = termRef.current?.fit()
          if (size && size.cols > 0 && size.rows > 0) {
            conn.resize(size.cols, size.rows)
            termRef.current?.focus()
            // Flush any data that arrived before terminal was ready
            if (pendingDataRef.current.length > 0) {
              for (const pending of pendingDataRef.current) {
                termRef.current?.write(pending)
              }
              pendingDataRef.current = []
            }
          } else if (retries > 0) {
            setTimeout(() => sendSize(retries - 1), 100)
          }
          // No fallback — if terminal isn't visible, the isActive effect
          // will send the resize when the tab becomes active
        }
        sendSize()
      },
      onWriteStateChange: (writer) => {
        setIsWriter(writer)
      },
      onSessionsChange: () => {
        onSessionsChangeRef.current?.()
      },
      onResizeNotify: (cols, rows) => {
        // Server is telling us the terminal size changed (tmux-style min dimensions)
        // Resize our xterm to match so display is correct
        console.log('[useTerminal] onResizeNotify:', cols, 'x', rows, 'termRef ready:', !!termRef.current)
        termRef.current?.resize(cols, rows)
      },
      onClientCount: (count) => {
        setClientCount(count)
      },
    }, sessionId)

    connRef.current = conn
    setConnected(true)

    return () => {
      conn.close()
      connRef.current = null
      pendingDataRef.current = []
    }
  }, [token, sessionId])

  // When tab becomes hidden, send 0x0 resize to exclude this client
  // from the session's min-size calculation. The XTerm isActive effect
  // will send proper dimensions when the tab becomes visible again.
  // useLayoutEffect runs before paint, preventing a single-frame "Viewer Mode" flash
  useLayoutEffect(() => {
    if (isActive) {
      // Optimistically assume writer to avoid a "Viewer Mode" flash during the
      // debounce + RTT before the server re-confirms our write state.
      // The server re-sends FrameWriteState when we transition from 0x0 to real dims,
      // so a genuine viewer will be corrected.
      setIsWriter(true)
      return
    }
    // Cancel any pending debounced resize so it doesn't overwrite our 0x0
    if (resizeTimeoutRef.current) {
      clearTimeout(resizeTimeoutRef.current)
      resizeTimeoutRef.current = null
    }
    connRef.current?.resize(0, 0)
  }, [isActive])

  // Handle user input
  // Filter out terminal response sequences that shouldn't be sent as user input
  const handleData = useCallback((data: string) => {
    // Filter out terminal query responses:
    // - DA1 responses: \e[?...c (e.g., \e[?1;2c)
    // - DA2 responses: \e[>...c
    // - Cursor position reports: \e[row;colR
    // - DECRPM (mode reports): \e[?...;...$y
    // - Focus reports: \e[I or \e[O
    // - DCS responses: \eP...\e\ (device control strings)
    // - OSC responses: \e]...\x07 or \e]...\e\
    const filtered = data
      .replace(/\x1b\[\?[\d;]*c/g, '')              // DA1
      .replace(/\x1b\[>[\d;]*c/g, '')               // DA2
      .replace(/\x1b\[\d+;\d+R/g, '')               // CPR (cursor position report)
      .replace(/\x1b\[\?[\d;]+\$y/g, '')            // DECRPM (mode report)
      .replace(/\x1b\[[IO]/g, '')                   // Focus in/out
      .replace(/\x1bP[^\x1b]*\x1b\\/g, '')          // DCS (device control string)
      .replace(/\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)/g, '') // OSC (operating system command)
      .replace(/\x1b\[[\d;]*_/g, '')                // APC-like sequences
    if (filtered) {
      connRef.current?.send(filtered)
    }
  }, [])

  // Handle resize with debouncing
  const resizeTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const handleResize = useCallback((cols: number, rows: number) => {
    if (resizeTimeoutRef.current) {
      clearTimeout(resizeTimeoutRef.current)
    }
    resizeTimeoutRef.current = setTimeout(() => {
      connRef.current?.resize(cols, rows)
      resizeTimeoutRef.current = null
    }, 50)
  }, [])

  // Handle file drop - upload files sequentially
  const handleFileDrop = useCallback(async (files: File[]) => {
    if (!connRef.current || files.length === 0) return

    for (const file of files) {
      try {
        setFileUpload({
          uploading: true,
          filename: file.name,
          bytesUploaded: 0,
          totalBytes: file.size,
        })

        const callbacks: FileTransferCallbacks = {
          onProgress: (bytesUploaded, totalBytes) => {
            setFileUpload(prev => prev ? { ...prev, bytesUploaded } : null)
            onFileProgressRef.current?.(bytesUploaded, totalBytes)
          },
          onComplete: (filename) => {
            setFileUpload(null)
            onFileCompleteRef.current?.(filename)
          },
          onError: (error) => {
            setFileUpload(null)
            onFileErrorRef.current?.(error)
          },
        }

        await connRef.current.sendFile(file, callbacks)
      } catch (err) {
        setFileUpload(null)
        const message = err instanceof Error ? err.message : 'Upload failed'
        onFileErrorRef.current?.(message)
      }
    }
  }, [])

  return {
    termRef,
    connected,
    exitCode,
    fileUpload,
    isWriter,
    clientCount,
    handleData,
    handleResize,
    handleFileDrop,
  }
}
