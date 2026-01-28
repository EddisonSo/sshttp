import type { TerminalTheme } from './itermThemeParser'
import { parseItermTheme } from './itermThemeParser'
import { api } from './api'

const THEMES_CACHE_KEY = 'terminal-themes-cache'
const ACTIVE_THEME_KEY = 'terminal-active-theme'

export const DEFAULT_THEME: TerminalTheme = {
  name: 'Default',
  background: '#1a1a2e',
  foreground: '#eaeaea',
  cursor: '#eaeaea',
  cursorAccent: '#1a1a2e',
  selectionBackground: '#3d3d5c',
  black: '#000000',
  red: '#cc0000',
  green: '#4e9a06',
  yellow: '#c4a000',
  blue: '#3465a4',
  magenta: '#75507b',
  cyan: '#06989a',
  white: '#d3d7cf',
  brightBlack: '#555753',
  brightRed: '#ef2929',
  brightGreen: '#8ae234',
  brightYellow: '#fce94f',
  brightBlue: '#729fcf',
  brightMagenta: '#ad7fa8',
  brightCyan: '#34e2e2',
  brightWhite: '#eeeeec',
}

// Cache management
export function getCachedThemes(): TerminalTheme[] {
  try {
    const stored = localStorage.getItem(THEMES_CACHE_KEY)
    if (!stored) return [DEFAULT_THEME]
    const themes = JSON.parse(stored) as TerminalTheme[]
    return [DEFAULT_THEME, ...themes]
  } catch {
    return [DEFAULT_THEME]
  }
}

export function setCachedThemes(themes: TerminalTheme[]): void {
  const customThemes = themes.filter(t => t.name !== 'Default')
  localStorage.setItem(THEMES_CACHE_KEY, JSON.stringify(customThemes))
}

export function addCachedTheme(theme: TerminalTheme): void {
  const themes = getCachedThemes().filter(t => t.name !== 'Default')
  const existing = themes.findIndex(t => t.name === theme.name)
  if (existing >= 0) {
    themes[existing] = theme
  } else {
    themes.push(theme)
  }
  localStorage.setItem(THEMES_CACHE_KEY, JSON.stringify(themes))
}

export function removeCachedTheme(name: string): void {
  if (name === 'Default') return
  const themes = getCachedThemes().filter(t => t.name !== 'Default' && t.name !== name)
  localStorage.setItem(THEMES_CACHE_KEY, JSON.stringify(themes))
}

// Active theme management
export function getActiveThemeName(): string {
  return localStorage.getItem(ACTIVE_THEME_KEY) || 'Default'
}

export function setActiveThemeName(name: string): void {
  localStorage.setItem(ACTIVE_THEME_KEY, name)
  applyThemeToDocument(name)
  window.dispatchEvent(new StorageEvent('storage', {
    key: ACTIVE_THEME_KEY,
    newValue: name,
  }))
}

// Server sync functions
export async function loadThemesFromServer(token: string): Promise<{ themes: TerminalTheme[], activeTheme: string }> {
  const response = await api.listThemes(token)
  const themes: TerminalTheme[] = [DEFAULT_THEME]

  // Load and parse each theme from server
  for (const themeInfo of response.themes) {
    try {
      const xmlContent = await api.getTheme(token, themeInfo.name)
      const parsed = parseItermTheme(xmlContent, themeInfo.name)
      themes.push(parsed)
    } catch (err) {
      console.error(`Failed to load theme ${themeInfo.name}:`, err)
    }
  }

  // Update cache
  setCachedThemes(themes)

  // Update active theme
  if (response.activeTheme) {
    localStorage.setItem(ACTIVE_THEME_KEY, response.activeTheme)
  }

  return { themes, activeTheme: response.activeTheme }
}

export async function saveThemeToServer(token: string, name: string, xmlContent: string): Promise<TerminalTheme> {
  await api.saveTheme(token, name, xmlContent)
  const parsed = parseItermTheme(xmlContent, name)
  addCachedTheme(parsed)
  return parsed
}

export async function deleteThemeFromServer(token: string, name: string): Promise<void> {
  await api.deleteTheme(token, name)
  removeCachedTheme(name)
  if (getActiveThemeName() === name) {
    setActiveThemeName('Default')
  }
}

export async function setActiveThemeOnServer(token: string, name: string): Promise<void> {
  await api.setActiveTheme(token, name)
  setActiveThemeName(name)
}

// Apply theme colors as CSS custom properties
export function applyThemeToDocument(name?: string): void {
  const themeName = name ?? getActiveThemeName()
  const themes = getCachedThemes()
  const theme = themes.find(t => t.name === themeName) || DEFAULT_THEME

  const root = document.documentElement
  root.style.setProperty('--theme-bg', theme.background)
  root.style.setProperty('--theme-fg', theme.foreground)
  root.style.setProperty('--theme-cursor', theme.cursor)
  root.style.setProperty('--theme-selection', theme.selectionBackground)

  // Derived colors for UI elements
  root.style.setProperty('--theme-bg-secondary', adjustBrightness(theme.background, 15))
  root.style.setProperty('--theme-bg-tertiary', adjustBrightness(theme.background, 25))
  root.style.setProperty('--theme-border', adjustBrightness(theme.background, 35))
  root.style.setProperty('--theme-fg-muted', adjustOpacity(theme.foreground, 0.6))
}

function adjustBrightness(hex: string, amount: number): string {
  const num = parseInt(hex.replace('#', ''), 16)
  const r = Math.min(255, Math.max(0, (num >> 16) + amount))
  const g = Math.min(255, Math.max(0, ((num >> 8) & 0x00FF) + amount))
  const b = Math.min(255, Math.max(0, (num & 0x0000FF) + amount))
  return `#${((1 << 24) + (r << 16) + (g << 8) + b).toString(16).slice(1)}`
}

function adjustOpacity(hex: string, opacity: number): string {
  const num = parseInt(hex.replace('#', ''), 16)
  const r = (num >> 16) & 255
  const g = (num >> 8) & 255
  const b = num & 255
  return `rgba(${r}, ${g}, ${b}, ${opacity})`
}

export function getActiveTheme(): TerminalTheme {
  const name = getActiveThemeName()
  const themes = getCachedThemes()
  return themes.find(t => t.name === name) || DEFAULT_THEME
}

// Legacy aliases for compatibility
export const loadThemes = getCachedThemes
export const saveTheme = addCachedTheme
export const deleteTheme = removeCachedTheme
export const setActiveTheme = setActiveThemeName
