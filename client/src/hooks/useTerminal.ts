import { useEffect, useRef, useCallback, useState } from 'react'
import { connectShell, ShellConnection, FileTransferCallbacks } from '../lib/ws'
import type { XTermHandle } from '../components/XTerm'

interface UseTerminalOptions {
  token: string
  sessionId?: string
  onExit?: (code: number) => void
  onError?: (error: Error) => void
  onFileProgress?: (bytesUploaded: number, totalBytes: number) => void
  onFileComplete?: (filename: string) => void
  onFileError?: (error: string) => void
}

interface FileUploadState {
  uploading: boolean
  filename: string
  bytesUploaded: number
  totalBytes: number
}

export function useTerminal({ token, sessionId, onExit, onError, onFileProgress, onFileComplete, onFileError }: UseTerminalOptions) {
  const termRef = useRef<XTermHandle>(null)
  const connRef = useRef<ShellConnection | null>(null)
  const [connected, setConnected] = useState(false)
  const [exitCode, setExitCode] = useState<number | null>(null)
  const [fileUpload, setFileUpload] = useState<FileUploadState | null>(null)

  // Store callbacks in refs to avoid reconnection on callback changes
  const onExitRef = useRef(onExit)
  const onErrorRef = useRef(onError)
  const onFileProgressRef = useRef(onFileProgress)
  const onFileCompleteRef = useRef(onFileComplete)
  const onFileErrorRef = useRef(onFileError)
  onExitRef.current = onExit
  onErrorRef.current = onError
  onFileProgressRef.current = onFileProgress
  onFileCompleteRef.current = onFileComplete
  onFileErrorRef.current = onFileError

  // Connect to shell
  useEffect(() => {
    if (!token) return

    const conn = connectShell(token, {
      onData: (data) => {
        termRef.current?.write(data)
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
        // Send initial size immediately on connection
        const size = termRef.current?.fit()
        if (size) {
          conn.resize(size.cols, size.rows)
        }
        termRef.current?.focus()
      },
    }, sessionId)

    connRef.current = conn
    setConnected(true)

    return () => {
      conn.close()
      connRef.current = null
    }
  }, [token, sessionId])

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
    handleData,
    handleResize,
    handleFileDrop,
  }
}
