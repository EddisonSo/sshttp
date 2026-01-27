// Convert base64url to ArrayBuffer
function base64urlToBuffer(base64url: string): ArrayBuffer {
  const base64 = base64url.replace(/-/g, '+').replace(/_/g, '/')
  const padding = '='.repeat((4 - (base64.length % 4)) % 4)
  const binary = atob(base64 + padding)
  const bytes = new Uint8Array(binary.length)
  for (let i = 0; i < binary.length; i++) {
    bytes[i] = binary.charCodeAt(i)
  }
  return bytes.buffer
}

// Convert ArrayBuffer to base64url
function bufferToBase64url(buffer: ArrayBuffer): string {
  const bytes = new Uint8Array(buffer)
  let binary = ''
  for (let i = 0; i < bytes.length; i++) {
    binary += String.fromCharCode(bytes[i])
  }
  return btoa(binary).replace(/\+/g, '-').replace(/\//g, '_').replace(/=/g, '')
}

// Convert server options to WebAuthn API format for registration
export function parseCreationOptions(
  options: Record<string, unknown>
): PublicKeyCredentialCreationOptions {
  const pubKey = options.publicKey as Record<string, unknown>

  return {
    challenge: base64urlToBuffer(pubKey.challenge as string),
    rp: pubKey.rp as PublicKeyCredentialRpEntity,
    user: {
      ...(pubKey.user as Record<string, unknown>),
      id: base64urlToBuffer((pubKey.user as Record<string, unknown>).id as string),
    } as PublicKeyCredentialUserEntity,
    pubKeyCredParams: pubKey.pubKeyCredParams as PublicKeyCredentialParameters[],
    timeout: pubKey.timeout as number | undefined,
    attestation: pubKey.attestation as AttestationConveyancePreference | undefined,
    authenticatorSelection: pubKey.authenticatorSelection as AuthenticatorSelectionCriteria | undefined,
  }
}

// Convert server options to WebAuthn API format for authentication
export function parseRequestOptions(
  options: Record<string, unknown>
): PublicKeyCredentialRequestOptions {
  const pubKey = options.publicKey as Record<string, unknown>

  const result: PublicKeyCredentialRequestOptions = {
    challenge: base64urlToBuffer(pubKey.challenge as string),
    timeout: pubKey.timeout as number | undefined,
    rpId: pubKey.rpId as string | undefined,
  }

  if (pubKey.allowCredentials) {
    result.allowCredentials = (pubKey.allowCredentials as Array<Record<string, unknown>>).map(
      (cred) => ({
        type: cred.type as PublicKeyCredentialType,
        id: base64urlToBuffer(cred.id as string),
        transports: cred.transports as AuthenticatorTransport[] | undefined,
      })
    )
  }

  return result
}

// Convert credential response to JSON for server
export function serializeCreationResponse(credential: PublicKeyCredential): Record<string, unknown> {
  const response = credential.response as AuthenticatorAttestationResponse

  return {
    id: credential.id,
    rawId: bufferToBase64url(credential.rawId),
    type: credential.type,
    response: {
      clientDataJSON: bufferToBase64url(response.clientDataJSON),
      attestationObject: bufferToBase64url(response.attestationObject),
    },
  }
}

// Convert assertion response to JSON for server
export function serializeAssertionResponse(credential: PublicKeyCredential): Record<string, unknown> {
  const response = credential.response as AuthenticatorAssertionResponse

  return {
    id: credential.id,
    rawId: bufferToBase64url(credential.rawId),
    type: credential.type,
    response: {
      clientDataJSON: bufferToBase64url(response.clientDataJSON),
      authenticatorData: bufferToBase64url(response.authenticatorData),
      signature: bufferToBase64url(response.signature),
      userHandle: response.userHandle ? bufferToBase64url(response.userHandle) : null,
    },
  }
}

// Create a new credential (registration)
export async function createCredential(
  options: PublicKeyCredentialCreationOptions
): Promise<PublicKeyCredential> {
  const credential = await navigator.credentials.create({
    publicKey: options,
  })

  if (!credential) {
    throw new Error('Failed to create credential')
  }

  return credential as PublicKeyCredential
}

// Get an existing credential (authentication)
export async function getCredential(
  options: PublicKeyCredentialRequestOptions
): Promise<PublicKeyCredential> {
  const credential = await navigator.credentials.get({
    publicKey: options,
  })

  if (!credential) {
    throw new Error('Failed to get credential')
  }

  return credential as PublicKeyCredential
}

// Check if WebAuthn is supported
export function isWebAuthnSupported(): boolean {
  return (
    typeof window !== 'undefined' &&
    'PublicKeyCredential' in window &&
    typeof navigator.credentials?.create === 'function' &&
    typeof navigator.credentials?.get === 'function'
  )
}
