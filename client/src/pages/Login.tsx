import { useState, useEffect } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, ApiError } from '../lib/api'
import {
  isWebAuthnSupported,
  parseRequestOptions,
  getCredential,
  serializeAssertionResponse,
} from '../lib/webauthn'

export default function Login() {
  const navigate = useNavigate()

  const [username, setUsername] = useState('')
  const [status, setStatus] = useState<'idle' | 'loading' | 'error'>('idle')
  const [error, setError] = useState('')

  useEffect(() => {
    if (!isWebAuthnSupported()) {
      setError('WebAuthn is not supported in this browser')
      setStatus('error')
    }

    // Check if already logged in
    const token = sessionStorage.getItem('accessToken')
    if (token) {
      navigate('/terminal')
    }
  }, [navigate])

  const handleLogin = async () => {
    if (!username.trim()) {
      setError('Username is required')
      return
    }

    setStatus('loading')
    setError('')

    try {
      // Step 1: Begin authentication
      const beginRes = await api.authBegin({ username: username.trim() })

      // Step 2: Get credential from authenticator
      const options = parseRequestOptions(beginRes.options as unknown as Record<string, unknown>)
      const credential = await getCredential(options)

      // Step 3: Finish authentication
      const serialized = serializeAssertionResponse(credential)
      const finishRes = await api.authFinish({
        state: beginRes.state,
        credential: serialized,
      })

      // Store token and redirect
      sessionStorage.setItem('accessToken', finishRes.accessToken)
      navigate('/terminal')
    } catch (err) {
      setStatus('error')
      if (err instanceof ApiError) {
        if (err.status === 401) {
          setError('Authentication failed. Please check your username and try again.')
        } else {
          setError(err.message)
        }
      } else if (err instanceof Error) {
        if (err.name === 'NotAllowedError') {
          setError('Authentication was cancelled or timed out')
        } else {
          setError(err.message)
        }
      } else {
        setError('Authentication failed')
      }
    }
  }

  const handleKeyPress = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter' && status !== 'loading') {
      handleLogin()
    }
  }

  return (
    <div className="flex min-h-screen items-center justify-center">
      <div className="w-full max-w-md p-8">
        <h1 className="mb-2 text-center text-3xl font-bold">sshttp</h1>
        <p className="mb-8 text-center text-[var(--theme-fg-muted)]">Secret Shell over HTTP</p>

        <div className="space-y-6">
          <div>
            <label htmlFor="username" className="mb-2 block text-sm text-[var(--theme-fg-muted)]">
              Username
            </label>
            <input
              type="text"
              id="username"
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              onKeyPress={handleKeyPress}
              className="w-full rounded-lg border border-[var(--theme-border)] bg-[var(--theme-bg-secondary)] px-4 py-3 text-[var(--theme-fg)] focus:border-blue-500 focus:outline-none"
              placeholder="Enter your username"
              disabled={status === 'loading'}
              autoFocus
            />
          </div>

          {error && (
            <div className="rounded-lg bg-red-900/50 p-3 text-sm text-red-400">{error}</div>
          )}

          <button
            onClick={handleLogin}
            disabled={status === 'loading' || (status === 'error' && !isWebAuthnSupported())}
            className="w-full rounded-lg bg-blue-600 px-4 py-3 font-medium text-white transition hover:bg-blue-700 disabled:cursor-not-allowed disabled:opacity-50"
          >
            {status === 'loading' ? 'Authenticating...' : 'Login with Passkey'}
          </button>

          <p className="text-center text-sm text-[var(--theme-fg-muted)]">
            You will be prompted to authenticate using your registered passkey.
          </p>
        </div>
      </div>
    </div>
  )
}
