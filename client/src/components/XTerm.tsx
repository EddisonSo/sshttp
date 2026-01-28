import { useEffect, useRef, useImperativeHandle, forwardRef, useState, useCallback } from 'react'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import { WebglAddon } from '@xterm/addon-webgl'
import { ClipboardAddon } from '@xterm/addon-clipboard'
import '@xterm/xterm/css/xterm.css'
import type { TerminalTheme } from '../lib/itermThemeParser'

export interface XTermHandle {
  write: (data: string) => void
  fit: () => { cols: number; rows: number }
  focus: () => void
}

interface XTermProps {
  onData?: (data: string) => void
  onResize?: (cols: number, rows: number) => void
  onFileDrop?: (files: File[]) => void
  theme?: TerminalTheme
  fontFamily?: string
  fontSize?: number
  isActive?: boolean
}

const DEFAULT_FONT_FAMILY = 'monospace'

// Filter out terminal response sequences that get echoed back from the PTY
// Preserves OSC52 (clipboard) sequences which are needed for copy/paste
function filterTerminalResponses(data: string): string {
  return data
    .replace(/\x1b\[\?[\d;]*c/g, '')              // DA1 (device attributes)
    .replace(/\x1b\[>[\d;]*c/g, '')               // DA2 (secondary device attributes)
    .replace(/\x1b\[\d+;\d+R/g, '')               // CPR (cursor position report)
    .replace(/\x1b\[\?[\d;]+\$y/g, '')            // DECRPM (mode report)
    .replace(/\x1b\[[IO]/g, '')                   // Focus in/out reports
    .replace(/\x1bP[^\x1b]*\x1b\\/g, '')          // DCS (device control string)
    .replace(/\x1b\](?!52;)[^\x07\x1b]*(?:\x07|\x1b\\)/g, '') // OSC except OSC52 (clipboard)
    .replace(/\x1b\[[\d;]*_/g, '')                // APC-like sequences
}

const DEFAULT_FONT_SIZE = 14

const XTerm = forwardRef<XTermHandle, XTermProps>(({ onData, onResize, onFileDrop, theme, fontFamily, fontSize = DEFAULT_FONT_SIZE, isActive = true }, ref) => {
  const containerRef = useRef<HTMLDivElement>(null)
  const terminalRef = useRef<Terminal | null>(null)
  const fitAddonRef = useRef<FitAddon | null>(null)
  const webglAddonRef = useRef<WebglAddon | null>(null)
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
      const filtered = filterTerminalResponses(data)
      if (filtered) {
        terminalRef.current?.write(filtered)
      }
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
      fontFamily: fontFamily || DEFAULT_FONT_FAMILY,
      fontSize: fontSize,
      fontWeight: '400',
      fontWeightBold: '700',
      theme: theme ? {
        background: theme.background,
        foreground: theme.foreground,
        cursor: theme.cursor,
        cursorAccent: theme.cursorAccent,
        selectionBackground: theme.selectionBackground,
        black: theme.black,
        red: theme.red,
        green: theme.green,
        yellow: theme.yellow,
        blue: theme.blue,
        magenta: theme.magenta,
        cyan: theme.cyan,
        white: theme.white,
        brightBlack: theme.brightBlack,
        brightRed: theme.brightRed,
        brightGreen: theme.brightGreen,
        brightYellow: theme.brightYellow,
        brightBlue: theme.brightBlue,
        brightMagenta: theme.brightMagenta,
        brightCyan: theme.brightCyan,
        brightWhite: theme.brightWhite,
      } : {
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

    // Load clipboard addon for OSC52 support
    const clipboardAddon = new ClipboardAddon()
    terminal.loadAddon(clipboardAddon)

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

    // Wait for the specific font to be loaded before initializing WebGL and fitting
    // This ensures proper character width measurement - WebGL addon caches font metrics on init
    const initialFontFamily = fontFamily || DEFAULT_FONT_FAMILY
    const primaryFont = initialFontFamily.split(',')[0].trim().replace(/^["']|["']$/g, '')

    const initializeWithFont = () => {
      if (!terminalRef.current) return

      // Load WebGL addon AFTER font is ready - it caches glyph textures on init
      try {
        const webglAddon = new WebglAddon()
        terminal.loadAddon(webglAddon)
        webglAddonRef.current = webglAddon
      } catch {
        // WebGL not available, fallback to canvas renderer
        webglAddonRef.current = null
      }

      requestAnimationFrame(() => {
        fitAddon.fit()
        if (onResize) {
          onResize(terminal.cols, terminal.rows)
        }
      })
    }

    // For generic families like 'monospace', load() resolves immediately
    // For web fonts, it waits for them to load
    document.fonts.load(`400 ${fontSize}px "${primaryFont}"`).then(initializeWithFont).catch(initializeWithFont)

    return () => {
      window.removeEventListener('resize', handleResize)
      terminal.dispose()
    }
  }, [onData, onResize])

  // Handle theme changes after mount
  useEffect(() => {
    if (!terminalRef.current || !theme) return
    terminalRef.current.options.theme = {
      background: theme.background,
      foreground: theme.foreground,
      cursor: theme.cursor,
      cursorAccent: theme.cursorAccent,
      selectionBackground: theme.selectionBackground,
      black: theme.black,
      red: theme.red,
      green: theme.green,
      yellow: theme.yellow,
      blue: theme.blue,
      magenta: theme.magenta,
      cyan: theme.cyan,
      white: theme.white,
      brightBlack: theme.brightBlack,
      brightRed: theme.brightRed,
      brightGreen: theme.brightGreen,
      brightYellow: theme.brightYellow,
      brightBlue: theme.brightBlue,
      brightMagenta: theme.brightMagenta,
      brightCyan: theme.brightCyan,
      brightWhite: theme.brightWhite,
    }
  }, [theme])

  // Handle font changes after mount
  useEffect(() => {
    if (!terminalRef.current) return

    const newFontFamily = fontFamily || DEFAULT_FONT_FAMILY

    // Extract the primary font name from the family string (e.g., "FontName" from '"FontName", fallback')
    const primaryFont = newFontFamily.split(',')[0].trim().replace(/^["']|["']$/g, '')

    const applyFont = () => {
      if (!terminalRef.current) return

      // Set the new font family
      terminalRef.current.options.fontFamily = newFontFamily

      // Dispose and recreate WebGL addon to ensure proper font metrics
      // The WebGL renderer caches font measurements that don't update on font change
      if (webglAddonRef.current) {
        try {
          webglAddonRef.current.dispose()
        } catch {
          // Ignore disposal errors
        }
        webglAddonRef.current = null
      }

      // Recreate WebGL addon with new font
      try {
        const webglAddon = new WebglAddon()
        terminalRef.current.loadAddon(webglAddon)
        webglAddonRef.current = webglAddon
      } catch {
        // WebGL not available
      }

      // Force terminal to recalculate character dimensions
      requestAnimationFrame(() => {
        if (!terminalRef.current || !fitAddonRef.current) return
        // Trigger a resize to force recalculation of character metrics
        const { cols, rows } = terminalRef.current
        terminalRef.current.resize(cols, rows)
        fitAddonRef.current.fit()
      })
    }

    // Wait for the specific font to be loaded before applying to terminal
    // This prevents xterm.js from measuring character widths with fallback fonts
    document.fonts.load(`400 ${fontSize}px "${primaryFont}"`).then(applyFont).catch(applyFont)
  }, [fontFamily])

  // Handle font size changes after mount
  useEffect(() => {
    if (!terminalRef.current) return
    terminalRef.current.options.fontSize = fontSize

    // Recreate WebGL addon to handle new font size measurements
    if (webglAddonRef.current) {
      try {
        webglAddonRef.current.dispose()
      } catch {
        // Ignore disposal errors
      }
      webglAddonRef.current = null
    }

    try {
      const webglAddon = new WebglAddon()
      terminalRef.current.loadAddon(webglAddon)
      webglAddonRef.current = webglAddon
    } catch {
      // WebGL not available
    }

    requestAnimationFrame(() => {
      if (!terminalRef.current || !fitAddonRef.current) return
      fitAddonRef.current.fit()
      if (onResize) {
        onResize(terminalRef.current.cols, terminalRef.current.rows)
      }
    })
  }, [fontSize, onResize])

  // Re-fit terminal when tab becomes active (visible)
  useEffect(() => {
    if (!isActive || !terminalRef.current || !fitAddonRef.current) return

    // Use requestAnimationFrame to ensure the container has correct dimensions
    requestAnimationFrame(() => {
      if (!terminalRef.current || !fitAddonRef.current) return
      fitAddonRef.current.fit()
      if (onResize) {
        onResize(terminalRef.current.cols, terminalRef.current.rows)
      }
      terminalRef.current.focus()
    })
  }, [isActive, onResize])

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
        style={{ backgroundColor: theme?.background || '#1a1a2e' }}
      />
      {isDragging && (
        <div className="absolute inset-0 flex items-center justify-center bg-blue-500/20 ring-4 ring-inset ring-blue-500 pointer-events-none">
          <div className="rounded-lg bg-[var(--theme-bg-secondary)]/90 px-6 py-4 text-lg font-medium text-[var(--theme-fg)]">
            Drop files to upload
          </div>
        </div>
      )}
    </div>
  )
})

XTerm.displayName = 'XTerm'

export default XTerm
