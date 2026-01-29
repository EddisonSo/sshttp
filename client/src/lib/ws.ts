// Frame types matching server protocol
export const FrameType = {
  STDIN: 0x01,
  STDOUT: 0x02,
  RESIZE: 0x04,
  EXIT: 0x05,
  FILE_START: 0x10,
  FILE_CHUNK: 0x11,
  FILE_ACK: 0x12,
  WRITE_STATE: 0x20,
  SESSIONS_CHANGE: 0x21,
  RESIZE_NOTIFY: 0x22,
  CLIENT_COUNT: 0x23,
} as const

// File ACK status codes
export const FileAckStatus = {
  SUCCESS: 0x00,
  PROGRESS: 0x01,
  ERROR: 0x02,
} as const

// File transfer constants
const FILE_CHUNK_SIZE = 32 * 1024 // 32KB
const MAX_FILE_SIZE = 100 * 1024 * 1024 // 100MB

export interface FileTransferCallbacks {
  onProgress?: (bytesUploaded: number, totalBytes: number) => void
  onComplete?: (filename: string) => void
  onError?: (error: string) => void
}

export interface ShellConnection {
  send: (data: string) => void
  resize: (cols: number, rows: number) => void
  sendFile: (file: File, callbacks?: FileTransferCallbacks) => Promise<void>
  close: () => void
}

export interface ShellCallbacks {
  onData: (data: string) => void
  onExit: (code: number) => void
  onError: (error: Error) => void
  onClose: () => void
  onOpen?: () => void
  onWriteStateChange?: (isWriter: boolean) => void
  onSessionsChange?: () => void
  onResizeNotify?: (cols: number, rows: number) => void
  onClientCount?: (count: number) => void
}

export function connectShell(token: string, callbacks: ShellCallbacks, sessionId?: string): ShellConnection {
  const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  let wsUrl = `${protocol}//${window.location.host}/v1/shell/stream?token=${encodeURIComponent(token)}`
  if (sessionId) {
    wsUrl += `&sessionId=${encodeURIComponent(sessionId)}`
  }

  const ws = new WebSocket(wsUrl)
  ws.binaryType = 'arraybuffer'

  // TextDecoder for converting binary to string
  const textDecoder = new TextDecoder('utf-8')

  // Track if session has exited (suppress errors after clean exit)
  let exited = false

  // File transfer state
  let fileTransferCallbacks: FileTransferCallbacks | null = null
  let fileTransferResolve: (() => void) | null = null
  let fileTransferReject: ((error: Error) => void) | null = null
  let fileTransferBytesUploaded = 0
  let fileTransferTotalBytes = 0

  ws.onopen = () => {
    console.log('[ws] WebSocket connected')
    callbacks.onOpen?.()

    // Periodic state check for debugging
    const stateCheck = setInterval(() => {
      const states = ['CONNECTING', 'OPEN', 'CLOSING', 'CLOSED']
      console.log('[ws] WebSocket state:', states[ws.readyState], 'onmessage:', !!ws.onmessage, 'bufferedAmount:', ws.bufferedAmount)
      if (ws.readyState === WebSocket.CLOSED) {
        clearInterval(stateCheck)
      }
    }, 5000)
  }

  // Close WebSocket on page unload to ensure clean disconnect
  const handleBeforeUnload = () => {
    ws.close(1000, 'page unload')
  }
  window.addEventListener('beforeunload', handleBeforeUnload)

  ws.onmessage = (event) => {
    try {
      const data = new Uint8Array(event.data as ArrayBuffer)
      if (data.length < 1) return

      const frameType = data[0]
      const payload = data.slice(1)

      // Log all frame types for debugging
      const frameNames: Record<number, string> = {
        0x01: 'STDIN', 0x02: 'STDOUT', 0x04: 'RESIZE', 0x05: 'EXIT',
        0x20: 'WRITE_STATE', 0x21: 'SESSIONS_CHANGE', 0x22: 'RESIZE_NOTIFY', 0x23: 'CLIENT_COUNT'
      }
      console.log('[ws] frame received:', frameNames[frameType] || frameType, 'payload:', payload.length, 'bytes')

      switch (frameType) {
      case FrameType.STDOUT:
        // Use stream mode to handle multi-byte characters split across chunks
        callbacks.onData(textDecoder.decode(payload, { stream: true }))
        break

      case FrameType.EXIT:
        exited = true
        if (payload.length >= 4) {
          const view = new DataView(payload.buffer, payload.byteOffset)
          const exitCode = view.getUint32(0, false) // big endian
          callbacks.onExit(exitCode)
        }
        break

      case FrameType.FILE_ACK:
        if (payload.length >= 1) {
          const status = payload[0]
          const message = payload.length > 1 ? textDecoder.decode(payload.slice(1)) : ''

          switch (status) {
            case FileAckStatus.SUCCESS:
              fileTransferCallbacks?.onComplete?.(message)
              fileTransferResolve?.()
              fileTransferCallbacks = null
              fileTransferResolve = null
              fileTransferReject = null
              break

            case FileAckStatus.PROGRESS:
              fileTransferCallbacks?.onProgress?.(fileTransferBytesUploaded, fileTransferTotalBytes)
              break

            case FileAckStatus.ERROR:
              fileTransferCallbacks?.onError?.(message || 'Upload failed')
              fileTransferReject?.(new Error(message || 'Upload failed'))
              fileTransferCallbacks = null
              fileTransferResolve = null
              fileTransferReject = null
              break
          }
        }
        break

      case FrameType.WRITE_STATE:
        if (payload.length >= 1) {
          const isWriter = payload[0] === 1
          callbacks.onWriteStateChange?.(isWriter)
        }
        break

      case FrameType.SESSIONS_CHANGE:
        callbacks.onSessionsChange?.()
        break

      case FrameType.RESIZE_NOTIFY:
        if (payload.length >= 4) {
          const view = new DataView(payload.buffer, payload.byteOffset)
          const cols = view.getUint16(0, false) // big endian
          const rows = view.getUint16(2, false) // big endian
          callbacks.onResizeNotify?.(cols, rows)
        }
        break

      case FrameType.CLIENT_COUNT:
        if (payload.length >= 2) {
          const view = new DataView(payload.buffer, payload.byteOffset)
          const count = view.getUint16(0, false) // big endian
          callbacks.onClientCount?.(count)
        }
        break
    }
    } catch (err) {
      console.error('[ws] Error in onmessage handler:', err)
    }
  }

  ws.onerror = (e) => {
    console.error('[ws] WebSocket error:', e)
    if (!exited) {
      callbacks.onError(new Error('WebSocket error'))
    }
  }

  ws.onclose = (e) => {
    console.log('[ws] WebSocket closed:', e.code, e.reason, 'wasClean:', e.wasClean)
    window.removeEventListener('beforeunload', handleBeforeUnload)
    if (!exited) {
      callbacks.onClose()
    }
    // Reject any pending file transfer
    if (fileTransferReject) {
      fileTransferReject(new Error('Connection closed'))
      fileTransferCallbacks = null
      fileTransferResolve = null
      fileTransferReject = null
    }
  }

  const send = (data: string) => {
    if (ws.readyState !== WebSocket.OPEN) return

    const encoded = new TextEncoder().encode(data)
    const frame = new Uint8Array(1 + encoded.length)
    frame[0] = FrameType.STDIN
    frame.set(encoded, 1)
    ws.send(frame)
  }

  const resize = (cols: number, rows: number) => {
    if (ws.readyState !== WebSocket.OPEN) return

    const frame = new Uint8Array(5)
    frame[0] = FrameType.RESIZE
    const view = new DataView(frame.buffer)
    view.setUint16(1, cols, false) // big endian
    view.setUint16(3, rows, false) // big endian
    ws.send(frame)
  }

  const sendFile = async (file: File, transferCallbacks?: FileTransferCallbacks): Promise<void> => {
    if (ws.readyState !== WebSocket.OPEN) {
      throw new Error('Not connected')
    }

    if (file.size > MAX_FILE_SIZE) {
      throw new Error('File too large (max 100MB)')
    }

    // Store callbacks for ACK handler
    fileTransferCallbacks = transferCallbacks || null
    fileTransferBytesUploaded = 0
    fileTransferTotalBytes = file.size

    return new Promise((resolve, reject) => {
      fileTransferResolve = resolve
      fileTransferReject = reject

      // Send FILE_START frame: [0x10][size:u32][name_len:u16][name:utf8]
      const nameBytes = new TextEncoder().encode(file.name)
      const startFrame = new Uint8Array(1 + 4 + 2 + nameBytes.length)
      startFrame[0] = FrameType.FILE_START
      const startView = new DataView(startFrame.buffer)
      startView.setUint32(1, file.size, false) // big endian
      startView.setUint16(5, nameBytes.length, false) // big endian
      startFrame.set(nameBytes, 7)
      ws.send(startFrame)

      // Read and send file in chunks
      const reader = new FileReader()
      let offset = 0

      const sendNextChunk = () => {
        if (offset >= file.size) {
          return // All chunks sent, wait for final ACK
        }

        const chunkSize = Math.min(FILE_CHUNK_SIZE, file.size - offset)
        const blob = file.slice(offset, offset + chunkSize)
        reader.readAsArrayBuffer(blob)
      }

      reader.onload = () => {
        if (!reader.result) return

        const chunkData = new Uint8Array(reader.result as ArrayBuffer)

        // Send FILE_CHUNK frame: [0x11][offset:u32][data...]
        const chunkFrame = new Uint8Array(1 + 4 + chunkData.length)
        chunkFrame[0] = FrameType.FILE_CHUNK
        const chunkView = new DataView(chunkFrame.buffer)
        chunkView.setUint32(1, offset, false) // big endian
        chunkFrame.set(chunkData, 5)
        ws.send(chunkFrame)

        offset += chunkData.length
        fileTransferBytesUploaded = offset

        // Send next chunk
        sendNextChunk()
      }

      reader.onerror = () => {
        reject(new Error('Failed to read file'))
        fileTransferCallbacks = null
        fileTransferResolve = null
        fileTransferReject = null
      }

      // Start sending chunks
      sendNextChunk()
    })
  }

  const close = () => {
    exited = true // Suppress error/close callbacks on intentional close
    window.removeEventListener('beforeunload', handleBeforeUnload)
    ws.close()
  }

  return { send, resize, sendFile, close }
}
