import { api } from './api'

export interface CustomFont {
  name: string
  url?: string  // URL to load the font from server
  ext?: string  // File extension (ttf, otf, woff, woff2)
  error?: string  // Error message if font failed to load
}

const FONTS_CACHE_KEY = 'terminal-fonts-cache'
const ACTIVE_FONT_KEY = 'terminal-active-font'
const FONT_SIZE_KEY = 'terminal-font-size'

export const DEFAULT_FONT_SIZE = 14
export const MIN_FONT_SIZE = 8
export const MAX_FONT_SIZE = 32

// Track fonts that have been successfully loaded
const loadedFonts = new Set<string>()

export const DEFAULT_FONT = 'monospace'
// Only generic CSS font - users can upload custom fonts via settings
export const BUILTIN_FONTS = ['monospace']

// Preload all builtin fonts to ensure they're available
// Call this early in app initialization
export async function preloadBuiltinFonts(): Promise<void> {
  // Mark monospace as always available (generic CSS family)
  loadedFonts.add('monospace')
}

// Cache management
export function getCachedFonts(): CustomFont[] {
  try {
    const stored = localStorage.getItem(FONTS_CACHE_KEY)
    if (!stored) return []
    return JSON.parse(stored) as CustomFont[]
  } catch {
    return []
  }
}

export function setCachedFonts(fonts: CustomFont[]): void {
  localStorage.setItem(FONTS_CACHE_KEY, JSON.stringify(fonts))
}

export function addCachedFont(font: CustomFont): void {
  const fonts = getCachedFonts()
  const existing = fonts.findIndex(f => f.name === font.name)
  if (existing >= 0) {
    fonts[existing] = font
  } else {
    fonts.push(font)
  }
  localStorage.setItem(FONTS_CACHE_KEY, JSON.stringify(fonts))
}

export function removeCachedFont(name: string): void {
  const fonts = getCachedFonts().filter(f => f.name !== name)
  localStorage.setItem(FONTS_CACHE_KEY, JSON.stringify(fonts))
}

// Active font management
export function getActiveFontName(): string {
  return localStorage.getItem(ACTIVE_FONT_KEY) || DEFAULT_FONT
}

export function setActiveFontName(name: string): void {
  localStorage.setItem(ACTIVE_FONT_KEY, name)
  window.dispatchEvent(new StorageEvent('storage', {
    key: ACTIVE_FONT_KEY,
    newValue: name,
  }))
}

// Font size management
export function getFontSize(): number {
  const stored = localStorage.getItem(FONT_SIZE_KEY)
  if (!stored) return DEFAULT_FONT_SIZE
  const size = parseInt(stored, 10)
  if (isNaN(size) || size < MIN_FONT_SIZE || size > MAX_FONT_SIZE) {
    return DEFAULT_FONT_SIZE
  }
  return size
}

export function setFontSize(size: number): void {
  const clamped = Math.min(MAX_FONT_SIZE, Math.max(MIN_FONT_SIZE, size))
  localStorage.setItem(FONT_SIZE_KEY, String(clamped))
  window.dispatchEvent(new StorageEvent('storage', {
    key: FONT_SIZE_KEY,
    newValue: String(clamped),
  }))
}

// Server sync functions
export async function loadFontsFromServer(token: string): Promise<{ fonts: CustomFont[], activeFont: string }> {
  const response = await api.listFonts(token)

  const fonts: CustomFont[] = response.fonts.map(f => ({
    name: f.name,
    url: api.getFontUrl(f.name),
    ext: f.ext,
  }))

  // Update cache
  setCachedFonts(fonts)

  // Update active font
  if (response.activeFont) {
    localStorage.setItem(ACTIVE_FONT_KEY, response.activeFont)
  }

  // Register fonts via JavaScript FontFace API (requires auth)
  await Promise.all(fonts.map(font =>
    font.url ? registerFontWithFetch(font.name, font.url, token) : Promise.resolve()
  ))

  return { fonts, activeFont: response.activeFont }
}

export async function uploadFontToServer(token: string, file: File): Promise<CustomFont> {
  const result = await api.uploadFont(token, file)
  const ext = result.ext || file.name.split('.').pop() || 'otf'
  const font: CustomFont = {
    name: result.name,
    url: api.getFontUrl(result.name),
    ext,
  }
  addCachedFont(font)

  // Register the font via JavaScript FontFace API (requires auth)
  if (font.url) {
    await registerFontWithFetch(font.name, font.url, token)
  }

  return font
}

export async function deleteFontFromServer(token: string, name: string): Promise<void> {
  await api.deleteFont(token, name)
  removeCachedFont(name)
  if (getActiveFontName() === name) {
    setActiveFontName(DEFAULT_FONT)
  }
}

export async function setActiveFontOnServer(token: string, name: string): Promise<void> {
  await api.setActiveFont(token, name)
  setActiveFontName(name)
}

// Register font via JavaScript FontFace API (requires auth token for fetch)
async function registerFontWithFetch(name: string, url: string, token: string): Promise<void> {
  if (loadedFonts.has(name)) {
    return
  }

  try {
    // Fetch font with auth header
    const response = await fetch(url, {
      headers: { Authorization: `Bearer ${token}` },
    })
    if (!response.ok) {
      console.error(`Failed to fetch font ${name}: ${response.status}`)
      return
    }

    const arrayBuffer = await response.arrayBuffer()

    const fontFace = new FontFace(name, arrayBuffer, {
      weight: '400',
      style: 'normal',
      display: 'swap',
    })
    const loaded = await fontFace.load()
    document.fonts.add(loaded)

    loadedFonts.add(name)
  } catch (err) {
    console.error(`Failed to load font ${name}:`, err)
  }
}

// Check if a font is available for use
export function isFontAvailable(name: string): boolean {
  // Builtin fonts are always available
  if (BUILTIN_FONTS.includes(name)) {
    return true
  }
  // Custom fonts are tracked via loadedFonts Set
  return loadedFonts.has(name)
}

export function registerAllFontsFromCache(): void {
  // No-op: fonts now require auth to load, so they must be registered
  // via loadFontsFromServer() which has access to the auth token
}

export function getFontFamily(fontName: string): string {
  return `"${fontName}", Menlo, Monaco, "Courier New", monospace`
}

// Legacy aliases for compatibility
export const loadFonts = getCachedFonts
export const setActiveFont = setActiveFontName

// Legacy function - no longer stores data URLs locally
export function registerAllFonts(): void {
  // Fonts are now registered when loaded from server
  // This is kept for compatibility but does nothing without a token
}
