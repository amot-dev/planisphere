import { act, createElement, type ReactElement } from 'react'
import { createRoot } from 'react-dom/client'

const activeUnmountCallbacks = new Set<() => void>()

/**
 * ButtonQueryOptions configures text matching and result indexing when querying
 * rendered buttons inside a mounted test container.
 */
export interface ButtonQueryOptions {
  exact?: boolean
  index?: number
}

/**
 * MountedTestRoot exposes query, rerender, and cleanup helpers for a React
 * element tree mounted into happy-dom.
 */
export interface MountedTestRoot {
  container: HTMLDivElement
  rerender: (nextElement: ReactElement) => void
  unmount: () => void
  queryButtonByText: (textMatch: string, options?: ButtonQueryOptions) => HTMLButtonElement | null
  findButtonByText: (textMatch: string, options?: ButtonQueryOptions) => HTMLButtonElement
}

/**
 * MountedHook exposes state inspection, prop updates, and unmount controls for
 * a custom React hook mounted inside happy-dom.
 */
export interface MountedHook<TProps, TResult> {
  getResult: () => TResult
  rerender: (nextProps: TProps) => void
  unmount: () => void
}

/**
 * FormControlValueOptions configures synthetic events dispatched when updating
 * an input, select, or textarea control in tests.
 */
export interface FormControlValueOptions {
  eventType?: 'input' | 'change'
  paste?: boolean
}

/**
 * unmountAllTestRoots unmounts and removes every test root still attached at
 * the end of a test case.
 */
export function unmountAllTestRoots(): void {
  for (const unmountCallback of Array.from(activeUnmountCallbacks)) {
    unmountCallback()
  }
  activeUnmountCallbacks.clear()
}

/**
 * mountTestRoot mounts a React element into a fresh container attached to
 * document.body and registers automatic teardown after each test.
 */
export function mountTestRoot(initialElement: ReactElement): MountedTestRoot {
  const container = document.createElement('div')
  document.body.appendChild(container)
  const root = createRoot(container)
  let isUnmounted = false

  const unmount = (): void => {
    if (isUnmounted) {
      return
    }
    isUnmounted = true
    activeUnmountCallbacks.delete(unmount)
    act(() => {
      root.unmount()
    })
    container.remove()
  }

  activeUnmountCallbacks.add(unmount)
  try {
    act(() => {
      root.render(initialElement)
    })
  } catch (renderError) {
    unmount()
    throw renderError
  }

  const queryButtonByText = (
    textMatch: string,
    options?: ButtonQueryOptions,
  ): HTMLButtonElement | null => {
    const buttons = Array.from(container.querySelectorAll('button'))
    const targetIndex = options?.index ?? 0
    if (options?.exact === true) {
      const exactMatches = buttons.filter((button) => button.textContent?.trim() === textMatch)
      return exactMatches[targetIndex] ?? null
    }
    if (options?.exact === false || options?.index !== undefined) {
      const substringMatches = buttons.filter((button) => button.textContent?.includes(textMatch))
      return substringMatches[targetIndex] ?? null
    }
    return (
      buttons.find((button) => button.textContent?.trim() === textMatch) ??
      buttons.find((button) => button.textContent?.includes(textMatch)) ??
      null
    )
  }

  const findButtonByText = (
    textMatch: string,
    options?: ButtonQueryOptions,
  ): HTMLButtonElement => {
    const matchedButton = queryButtonByText(textMatch, options)
    if (!matchedButton) {
      throw new Error(`Expected button matching "${textMatch}" to exist`)
    }
    return matchedButton
  }

  return {
    container,
    rerender: (nextElement: ReactElement) => {
      act(() => {
        root.render(nextElement)
      })
    },
    unmount,
    queryButtonByText,
    findButtonByText,
  }
}

/**
 * renderHook mounts a probe component executing useHookCallback and exposes its
 * latest return value alongside rerender and unmount helpers.
 */
export function renderHook<TResult>(useHookCallback: () => TResult): MountedHook<void, TResult>
export function renderHook<TProps, TResult>(
  useHookCallback: (props: TProps) => TResult,
  initialProps: NoInfer<TProps>,
): MountedHook<TProps, TResult>
export function renderHook<TProps, TResult>(
  useHookCallback: (props: TProps) => TResult,
  initialProps?: NoInfer<TProps>,
): MountedHook<TProps, TResult> {
  let latestResult!: TResult

  function HookProbe({ hookProps }: { hookProps: TProps }): null {
    latestResult = useHookCallback(hookProps)
    return null
  }

  const mountedRoot = mountTestRoot(
    createElement(HookProbe, { hookProps: initialProps as TProps }),
  )

  return {
    getResult: () => latestResult,
    rerender: (nextProps: TProps) => {
      mountedRoot.rerender(createElement(HookProbe, { hookProps: nextProps }))
    },
    unmount: mountedRoot.unmount,
  }
}

/**
 * setFormControlValue updates a form control through its native prototype value
 * setter and dispatches bubbling paste, input, or change events for React.
 */
export function setFormControlValue(
  element: HTMLInputElement | HTMLSelectElement | HTMLTextAreaElement,
  nextValue: string,
  options?: FormControlValueOptions,
): void {
  if (options?.paste) {
    element.dispatchEvent(new Event('paste', { bubbles: true }))
  }
  const controlPrototype =
    element instanceof HTMLSelectElement
      ? HTMLSelectElement.prototype
      : element instanceof HTMLTextAreaElement
        ? HTMLTextAreaElement.prototype
        : HTMLInputElement.prototype
  const valueDescriptor = Object.getOwnPropertyDescriptor(controlPrototype, 'value')
  valueDescriptor?.set?.call(element, nextValue)
  const resolvedEventType =
    options?.eventType ?? (element instanceof HTMLSelectElement ? 'change' : 'input')
  element.dispatchEvent(new Event(resolvedEventType, { bubbles: true }))
}
