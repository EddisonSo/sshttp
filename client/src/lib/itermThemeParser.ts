export interface TerminalTheme {
  name: string
  background: string
  foreground: string
  cursor: string
  cursorAccent: string
  selectionBackground: string
  black: string
  red: string
  green: string
  yellow: string
  blue: string
  magenta: string
  cyan: string
  white: string
  brightBlack: string
  brightRed: string
  brightGreen: string
  brightYellow: string
  brightBlue: string
  brightMagenta: string
  brightCyan: string
  brightWhite: string
}

function floatToHex(value: number): string {
  const clamped = Math.max(0, Math.min(1, value))
  const int = Math.round(clamped * 255)
  return int.toString(16).padStart(2, '0')
}

function extractColor(dict: Element): string | null {
  const keys = dict.querySelectorAll(':scope > key')
  let r = 0, g = 0, b = 0

  for (const key of keys) {
    const keyName = key.textContent?.trim()
    const valueEl = key.nextElementSibling
    if (!valueEl || valueEl.tagName !== 'real') continue

    const value = parseFloat(valueEl.textContent || '0')
    if (keyName === 'Red Component') r = value
    else if (keyName === 'Green Component') g = value
    else if (keyName === 'Blue Component') b = value
  }

  return `#${floatToHex(r)}${floatToHex(g)}${floatToHex(b)}`
}

function findColorDict(doc: Document, colorName: string): Element | null {
  const keys = doc.querySelectorAll('plist > dict > key')
  for (const key of keys) {
    if (key.textContent?.trim() === colorName) {
      const next = key.nextElementSibling
      if (next && next.tagName === 'dict') {
        return next
      }
    }
  }
  return null
}

export function parseItermTheme(xml: string, themeName: string): TerminalTheme {
  const parser = new DOMParser()
  const doc = parser.parseFromString(xml, 'application/xml')

  const parserError = doc.querySelector('parsererror')
  if (parserError) {
    throw new Error('Invalid XML format')
  }

  const getColor = (name: string, fallback: string): string => {
    const dict = findColorDict(doc, name)
    if (!dict) return fallback
    return extractColor(dict) || fallback
  }

  return {
    name: themeName,
    background: getColor('Background Color', '#1a1a2e'),
    foreground: getColor('Foreground Color', '#eaeaea'),
    cursor: getColor('Cursor Color', '#eaeaea'),
    cursorAccent: getColor('Cursor Text Color', '#1a1a2e'),
    selectionBackground: getColor('Selection Color', '#3d3d5c'),
    black: getColor('Ansi 0 Color', '#000000'),
    red: getColor('Ansi 1 Color', '#cc0000'),
    green: getColor('Ansi 2 Color', '#00cc00'),
    yellow: getColor('Ansi 3 Color', '#cccc00'),
    blue: getColor('Ansi 4 Color', '#0000cc'),
    magenta: getColor('Ansi 5 Color', '#cc00cc'),
    cyan: getColor('Ansi 6 Color', '#00cccc'),
    white: getColor('Ansi 7 Color', '#cccccc'),
    brightBlack: getColor('Ansi 8 Color', '#666666'),
    brightRed: getColor('Ansi 9 Color', '#ff0000'),
    brightGreen: getColor('Ansi 10 Color', '#00ff00'),
    brightYellow: getColor('Ansi 11 Color', '#ffff00'),
    brightBlue: getColor('Ansi 12 Color', '#0000ff'),
    brightMagenta: getColor('Ansi 13 Color', '#ff00ff'),
    brightCyan: getColor('Ansi 14 Color', '#00ffff'),
    brightWhite: getColor('Ansi 15 Color', '#ffffff'),
  }
}
