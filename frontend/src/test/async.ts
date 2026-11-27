/**
 * Deferred exposes an externally controllable Promise alongside its resolve and
 * reject callbacks for deterministic async test orchestration.
 */
export interface Deferred<T = void> {
  promise: Promise<T>
  resolve: (value: T) => void
  reject: (reason?: unknown) => void
}

/**
 * createDeferred constructs a Deferred promise whose settlement is controlled
 * explicitly by the caller.
 */
export function createDeferred<T = void>(): Deferred<T> {
  let resolvePromise!: (value: T) => void
  let rejectPromise!: (reason?: unknown) => void
  const promise = new Promise<T>((resolve, reject) => {
    resolvePromise = resolve
    rejectPromise = reject
  })
  return { promise, resolve: resolvePromise, reject: rejectPromise }
}

/**
 * flushMicrotasks drains queued Promise microtasks so chained async state
 * updates settle deterministically inside act blocks.
 */
export async function flushMicrotasks(ticks = 3): Promise<void> {
  for (let index = 0; index < ticks; index += 1) {
    await Promise.resolve()
  }
}
