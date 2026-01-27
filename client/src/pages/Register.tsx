import { useState, useEffect } from 'react'
import { useSearchParams, useNavigate } from 'react-router-dom'
import { api, ApiError } from '../lib/api'
import {
  isWebAuthnSupported,
  parseCreationOptions,
  createCredential,
  serializeCreationResponse,
} from '../lib/webauthn'

export default function Register() {
  const [searchParams] = useSearchParams()
  const navigate = useNavigate()
  const rid = searchParams.get('rid')

  const [username, setUsername] = useState<string | null>(null)
  const [isNewUser, setIsNewUser] = useState(true)
  const [status, setStatus] = useState<'loading' | 'ready' | 'registering' | 'success' | 'error'>('loading')
  const [error, setError] = useState('')

  // Fetch registration info on mount
  useEffect(() => {
    if (!rid) {
      setError('Missing registration ID')
      setStatus('error')
      return
    }

    if (!isWebAuthnSupported()) {
      setError('WebAuthn is not supported in this browser')
      setStatus('error')
      return
    }

    api.registerInfo(rid)
      .then((info) => {
        setUsername(info.username)
        setIsNewUser(info.isNewUser)
        setStatus('ready')
      })
      .catch((err) => {
        if (err instanceof ApiError) {
          setError(err.message)
        } else {
          setError('Failed to load registration')
        }
        setStatus('error')
      })
  }, [rid])

  const handleRegister = async () => {
    if (!rid || !username) return

    setStatus('registering')
    setError('')

    try {
      // Step 1: Begin registration
      const beginRes = await api.registerBegin({ rid, username })

      // Step 2: Create credential with authenticator
      const options = parseCreationOptions(beginRes.options as unknown as Record<string, unknown>)
      const credential = await createCredential(options)

      // Step 3: Finish registration
      const serialized = serializeCreationResponse(credential)
      await api.registerFinish({
        rid,
        state: beginRes.state,
        credential: serialized,
      })

      setStatus('success')

      // Redirect to login after short delay
      setTimeout(() => navigate('/login'), 2000)
    } catch (err) {
      setStatus('error')
      if (err instanceof ApiError) {
        setError(err.message)
      } else if (err instanceof Error) {
        if (err.name === 'NotAllowedError') {
          setError('Registration was cancelled or timed out')
        } else {
          setError(err.message)
        }
      } else {
        setError('Registration failed')
      }
    }
  }

  if (!rid) {
    return (
      <div className="flex min-h-screen items-center justify-center">
        <div className="w-full max-w-md p-8 text-center">
          <h1 className="mb-4 text-2xl font-bold text-red-400">Invalid Registration Link</h1>
          <p className="text-gray-400">Please use a valid registration link.</p>
        </div>
      </div>
    )
  }

  return (
    <div className="flex min-h-screen items-center justify-center">
      <div className="w-full max-w-md p-8">
        <h1 className="mb-2 text-center text-3xl font-bold">
          {isNewUser ? 'Register' : 'Add Passkey'}
        </h1>

        {username && (
          <p className="mb-8 text-center text-lg text-gray-400">{username}</p>
        )}

        {status === 'loading' && (
          <div className="text-center text-gray-400">Loading...</div>
        )}

        {status === 'success' ? (
          <div className="rounded-lg bg-green-900/50 p-4 text-center">
            <p className="text-green-400">
              {isNewUser ? 'Registration successful!' : 'Passkey added successfully!'}
            </p>
            <p className="mt-2 text-sm text-gray-400">Redirecting to login...</p>
          </div>
        ) : status === 'ready' || status === 'registering' || status === 'error' ? (
          <div className="space-y-6">
            {error && (
              <div className="rounded-lg bg-red-900/50 p-3 text-sm text-red-400">{error}</div>
            )}

            <button
              onClick={handleRegister}
              disabled={status === 'registering'}
              className="w-full rounded-lg bg-blue-600 px-4 py-3 font-medium text-white transition hover:bg-blue-700 disabled:cursor-not-allowed disabled:opacity-50"
            >
              {status === 'registering' ? 'Waiting for authenticator...' : 'Add Passkey'}
            </button>

            <p className="text-center text-sm text-gray-500">
              You will be prompted to create a passkey using your device's authentication.
            </p>
          </div>
        ) : null}
      </div>
    </div>
  )
}
