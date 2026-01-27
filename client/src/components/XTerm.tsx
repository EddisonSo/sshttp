import { useEffect, useRef, useImperativeHandle, forwardRef, useState, useCallback } from 'react'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import { WebglAddon } from '@xterm/addon-webgl'
import { ClipboardAddon } from '@xterm/addon-clipboard'
import '@xterm/xterm/css/xterm.css'

export interface XTermHandle {
  write: (data: string) => void
  fit: () => { cols: number; rows: number }
  focus: () => void
}

interface XTermProps {
  onData?: (data: string) => void
  onResize?: (cols: number, rows: number) => void
  onFileDrop?: (files: File[]) => void
}

const XTerm = forwardRef<XTermHandle, XTermProps>(({ onData, onResize, onFileDrop }, ref) => {
  const containerRef = useRef<HTMLDivElement>(null)
  const terminalRef = useRef<Terminal | null>(null)
  const fitAddonRef = useRef<FitAddon | null>(null)
  const [isDragging, setIsDragging] = useState(false)
  const dragCounterRef = useRef(0)

  const handleDragEnter = useCallback((e: React.DragEvent) => {
    e.preventDefault()
    e.stopPropagation()
    dragCounterRef.current++
    if (e.dataTransfer.types.includes('Files')) {
      setIsDragging(true)
    }
  }, [])

  const handleDragLeave = useCallback((e: React.DragEvent) => {
    e.preventDefault()
    e.stopPropagation()
    dragCounterRef.current--
    if (dragCounterRef.current === 0) {
      setIsDragging(false)
    }
  }, [])

  const handleDragOver = useCallback((e: React.DragEvent) => {
    e.preventDefault()
    e.stopPropagation()
  }, [])

  const handleDrop = useCallback((e: React.DragEvent) => {
    e.preventDefault()
    e.stopPropagation()
    setIsDragging(false)
    dragCounterRef.current = 0

    if (e.dataTransfer.files.length > 0 && onFileDrop) {
      const files = Array.from(e.dataTransfer.files)
      onFileDrop(files)
    }

    // Re-focus terminal after drop
    terminalRef.current?.focus()
  }, [onFileDrop])

  useImperativeHandle(ref, () => ({
    write: (data: string) => {
      terminalRef.current?.write(data)
    },
    fit: () => {
      fitAddonRef.current?.fit()
      const cols = terminalRef.current?.cols ?? 80
      const rows = terminalRef.current?.rows ?? 24
      return { cols, rows }
    },
    focus: () => {
      terminalRef.current?.focus()
    },
  }))

  useEffect(() => {
    if (!containerRef.current) return

    const terminal = new Terminal({
      cursorBlink: true,
      fontFamily: '"AdwaitaMono NF", Menlo, Monaco, "Courier New", monospace',
      fontSize: 14,
      theme: {
        background: '#1a1a2e',
        foreground: '#eaeaea',
        cursor: '#eaeaea',
        cursorAccent: '#1a1a2e',
        selectionBackground: '#3d3d5c',
      },
    })

    const fitAddon = new FitAddon()
    terminal.loadAddon(fitAddon)

    terminal.open(containerRef.current)

    // Try to load WebGL addon for better performance
    try {
      const webglAddon = new WebglAddon()
      terminal.loadAddon(webglAddon)
    } catch {
      // WebGL not available, fallback to canvas renderer
    }

    // Load clipboard addon for OSC52 support
    const clipboardAddon = new ClipboardAddon()
    terminal.loadAddon(clipboardAddon)

    fitAddon.fit()

    terminalRef.current = terminal
    fitAddonRef.current = fitAddon

    // Handle user input
    if (onData) {
      terminal.onData(onData)
    }

    // Handle resize
    const handleResize = () => {
      fitAddon.fit()
      if (onResize) {
        onResize(terminal.cols, terminal.rows)
      }
    }

    window.addEventListener('resize', handleResize)

    // Initial resize callback
    if (onResize) {
      onResize(terminal.cols, terminal.rows)
    }

    return () => {
      window.removeEventListener('resize', handleResize)
      terminal.dispose()
    }
  }, [onData, onResize])

  return (
    <div
      className="relative h-full w-full"
      onDragEnter={handleDragEnter}
      onDragLeave={handleDragLeave}
      onDragOver={handleDragOver}
      onDrop={handleDrop}
    >
      <div
        ref={containerRef}
        className="h-full w-full"
        style={{ backgroundColor: '#1a1a2e' }}
      />
      {isDragging && (
        <div className="absolute inset-0 flex items-center justify-center bg-blue-500/20 ring-4 ring-inset ring-blue-500 pointer-events-none">
          <div className="rounded-lg bg-gray-900/90 px-6 py-4 text-lg font-medium text-white">
            Drop files to upload
          </div>
        </div>
      )}
    </div>
  )
})

XTerm.displayName = 'XTerm'

export default XTerm
