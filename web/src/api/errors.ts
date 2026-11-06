import type { components } from './schema.gen'

export type ApiErrorType = components['schemas']['ErrorType']

// Client-side types that the API never sends.
export type ClientErrorType = 'network' | 'unknown'

/** An error response from the admin API, or a failure to reach it. */
export class ApiError extends Error {
  readonly status: number
  readonly type: ApiErrorType | ClientErrorType
  readonly parameter: string | undefined

  constructor(
    status: number,
    type: ApiErrorType | ClientErrorType,
    message: string,
    parameter?: string,
  ) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.type = type
    this.parameter = parameter
  }
}

const API_ERROR_TYPES: readonly string[] = [
  'invalid_parameter',
  'invalid_cursor',
  'invalid_id',
  'not_found',
  'internal',
] satisfies ApiErrorType[]

function isApiErrorType(value: unknown): value is ApiErrorType {
  return typeof value === 'string' && API_ERROR_TYPES.includes(value)
}

/** Builds an ApiError from a non-2xx response body (already parsed, or undefined). */
export function apiErrorFromBody(status: number, body: unknown): ApiError {
  const err =
    typeof body === 'object' && body !== null && 'error' in body
      ? (body as { error: unknown }).error
      : undefined
  if (typeof err === 'object' && err !== null) {
    const { type, message, parameter } = err as Record<string, unknown>
    if (isApiErrorType(type)) {
      return new ApiError(
        status,
        type,
        typeof message === 'string' ? message : type,
        typeof parameter === 'string' ? parameter : undefined,
      )
    }
  }
  // A proxy or an old server can answer without the error envelope.
  return new ApiError(status, status >= 500 ? 'internal' : 'unknown', `HTTP ${status}`)
}

/**
 * Maps an error to the inline message the UI shows. The message depends on
 * `type`, never on the server's `message` text, which is for logs.
 */
export function errorMessage(error: unknown): string {
  if (!(error instanceof ApiError)) {
    return 'Something went wrong. Try again.'
  }
  switch (error.type) {
    case 'invalid_parameter':
      return error.parameter
        ? `The filter "${error.parameter}" is not valid.`
        : 'A filter is not valid.'
    case 'invalid_cursor':
      return 'The list position has expired. Reload the first page.'
    case 'invalid_id':
      return 'This is not a valid id.'
    case 'not_found':
      return 'Not found. It may be outside the retention window.'
    case 'internal':
      return 'The server failed. Try again later.'
    case 'network':
      return 'Cannot reach the server. Check that llm-proxy is running.'
    case 'unknown':
      return 'Something went wrong. Try again.'
  }
}

/** Retrying cannot fix a 4xx answer; only transient failures are retried. */
export function isRetryable(error: unknown): boolean {
  return error instanceof ApiError && (error.type === 'network' || error.type === 'internal')
}
