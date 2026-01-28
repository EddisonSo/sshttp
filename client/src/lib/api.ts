const API_BASE = '/v1'

export interface RegisterInfoResponse {
  username: string
  isNewUser: boolean
}

export interface RegisterBeginRequest {
  rid: string
  username: string
}

export interface RegisterBeginResponse {
  options: PublicKeyCredentialCreationOptions
  state: string
}

export interface RegisterFinishRequest {
  rid: string
  state: string
  credential: PublicKeyCredential
}

export interface AuthBeginRequest {
  username: string
}

export interface AuthBeginResponse {
  options: PublicKeyCredentialRequestOptions
  state: string
}

export interface AuthFinishRequest {
  state: string
  credential: PublicKeyCredential
}

export interface AuthFinishResponse {
  accessToken: string
}

export interface KeyInfo {
  id: string
  name: string
  authenticatorType: string
  createdAt: string
}

export interface ListKeysResponse {
  keys: KeyInfo[]
}

export interface AddKeyBeginResponse {
  options: PublicKeyCredentialCreationOptions
  state: string
}

export interface SessionInfo {
  id: string
  name: string
  createdAt: string
  attached: boolean
}

export interface ListSessionsResponse {
  sessions: SessionInfo[]
}

export interface CreateSessionResponse {
  id: string
  name: string
}

export interface ThemeInfo {
  name: string
}

export interface ListThemesResponse {
  themes: ThemeInfo[]
  activeTheme: string
}

export interface FontInfo {
  name: string
  ext: string  // File extension (ttf, otf, woff, woff2)
}

export interface ListFontsResponse {
  fonts: FontInfo[]
  activeFont: string
}

export interface UserPrefs {
  activeTheme: string
  activeFont: string
}

class ApiError extends Error {
  constructor(public status: number, message: string) {
    super(message)
    this.name = 'ApiError'
  }
}

async function request<T>(path: string, options: RequestInit = {}): Promise<T> {
  const res = await fetch(`${API_BASE}${path}`, {
    ...options,
    headers: {
      'Content-Type': 'application/json',
      ...options.headers,
    },
  })

  if (!res.ok) {
    const text = await res.text()
    throw new ApiError(res.status, text || res.statusText)
  }

  // Handle empty responses
  const text = await res.text()
  if (!text) {
    return {} as T
  }
  return JSON.parse(text)
}

export const api = {
  registerInfo: (rid: string) =>
    request<RegisterInfoResponse>(`/register/info?rid=${encodeURIComponent(rid)}`),

  registerBegin: (data: RegisterBeginRequest) =>
    request<RegisterBeginResponse>('/register/begin', {
      method: 'POST',
      body: JSON.stringify(data),
    }),

  registerFinish: (data: { rid: string; state: string; credential: unknown }) =>
    request<{ success: boolean }>('/register/finish', {
      method: 'POST',
      body: JSON.stringify(data),
    }),

  authBegin: (data: AuthBeginRequest) =>
    request<AuthBeginResponse>('/auth/begin', {
      method: 'POST',
      body: JSON.stringify(data),
    }),

  authFinish: (data: { state: string; credential: unknown }) =>
    request<AuthFinishResponse>('/auth/finish', {
      method: 'POST',
      body: JSON.stringify(data),
    }),

  listKeys: (token: string) =>
    request<ListKeysResponse>('/settings/keys', {
      headers: { Authorization: `Bearer ${token}` },
    }),

  deleteKey: (token: string, id: string) =>
    request<void>('/settings/keys/delete', {
      method: 'POST',
      headers: { Authorization: `Bearer ${token}` },
      body: JSON.stringify({ id }),
    }),

  renameKey: (token: string, id: string, name: string) =>
    request<void>('/settings/keys/rename', {
      method: 'POST',
      headers: { Authorization: `Bearer ${token}` },
      body: JSON.stringify({ id, name }),
    }),

  addKeyBegin: (token: string) =>
    request<AddKeyBeginResponse>('/settings/keys/add/begin', {
      method: 'POST',
      headers: { Authorization: `Bearer ${token}` },
    }),

  addKeyFinish: (token: string, data: { state: string; credential: unknown }) =>
    request<void>('/settings/keys/add/finish', {
      method: 'POST',
      headers: { Authorization: `Bearer ${token}` },
      body: JSON.stringify(data),
    }),

  listSessions: (token: string) =>
    request<ListSessionsResponse>('/shell/sessions', {
      headers: { Authorization: `Bearer ${token}` },
    }),

  createSession: (token: string, name?: string) =>
    request<CreateSessionResponse>('/shell/sessions', {
      method: 'POST',
      headers: { Authorization: `Bearer ${token}` },
      body: JSON.stringify({ name }),
    }),

  renameSession: (token: string, id: string, name: string) =>
    request<void>('/shell/sessions/rename', {
      method: 'POST',
      headers: { Authorization: `Bearer ${token}` },
      body: JSON.stringify({ id, name }),
    }),

  deleteSession: (token: string, id: string) =>
    request<void>('/shell/sessions/delete', {
      method: 'POST',
      headers: { Authorization: `Bearer ${token}` },
      body: JSON.stringify({ id }),
    }),

  // Customization
  getPrefs: (token: string) =>
    request<UserPrefs>('/settings/prefs', {
      headers: { Authorization: `Bearer ${token}` },
    }),

  // Themes
  listThemes: (token: string) =>
    request<ListThemesResponse>('/settings/themes', {
      headers: { Authorization: `Bearer ${token}` },
    }),

  getTheme: async (token: string, name: string): Promise<string> => {
    const res = await fetch(`${API_BASE}/settings/themes/get?name=${encodeURIComponent(name)}`, {
      headers: { Authorization: `Bearer ${token}` },
    })
    if (!res.ok) {
      throw new ApiError(res.status, await res.text())
    }
    return res.text()
  },

  saveTheme: (token: string, name: string, content: string) =>
    request<void>('/settings/themes/save', {
      method: 'POST',
      headers: { Authorization: `Bearer ${token}` },
      body: JSON.stringify({ name, content }),
    }),

  deleteTheme: (token: string, name: string) =>
    request<void>('/settings/themes/delete', {
      method: 'POST',
      headers: { Authorization: `Bearer ${token}` },
      body: JSON.stringify({ name }),
    }),

  setActiveTheme: (token: string, name: string) =>
    request<void>('/settings/themes/active', {
      method: 'POST',
      headers: { Authorization: `Bearer ${token}` },
      body: JSON.stringify({ name }),
    }),

  // Fonts
  listFonts: (token: string) =>
    request<ListFontsResponse>('/settings/fonts', {
      headers: { Authorization: `Bearer ${token}` },
    }),

  // Font URL (requires auth - loaded via JavaScript fetch)
  getFontUrl: (name: string) =>
    `${API_BASE}/settings/fonts/get?name=${encodeURIComponent(name)}`,

  uploadFont: async (token: string, file: File): Promise<FontInfo> => {
    const formData = new FormData()
    formData.append('font', file)
    const res = await fetch(`${API_BASE}/settings/fonts/upload`, {
      method: 'POST',
      headers: { Authorization: `Bearer ${token}` },
      body: formData,
    })
    if (!res.ok) {
      throw new ApiError(res.status, await res.text())
    }
    return res.json()
  },

  deleteFont: (token: string, name: string) =>
    request<void>('/settings/fonts/delete', {
      method: 'POST',
      headers: { Authorization: `Bearer ${token}` },
      body: JSON.stringify({ name }),
    }),

  setActiveFont: (token: string, name: string) =>
    request<void>('/settings/fonts/active', {
      method: 'POST',
      headers: { Authorization: `Bearer ${token}` },
      body: JSON.stringify({ name }),
    }),
}

export { ApiError }
