// @vitest-environment happy-dom
import { act, createElement } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { app, modes } from '../../../api/models'

const mockGetStartCandidates = vi.fn()
const mockStartExpedition = vi.fn()

vi.mock('../../../api/backend', () => ({
  GetStartCandidates: (...args: unknown[]) => mockGetStartCandidates(...args),
  StartExpedition: (...args: unknown[]) => mockStartExpedition(...args),
}))

import { StartExpeditionModal } from './StartExpeditionModal'

;(globalThis as unknown as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true

describe('StartExpeditionModal async safety', () => {
  afterEach(() => vi.clearAllMocks())

  it('disables close and action buttons during StartExpedition and ignores candidate settlement after unmount', async () => {
    mockGetStartCandidates.mockResolvedValueOnce(
      app.StartCandidates.createFrom({
        systemName: 'Sol',
        visits: [{ uid: 1, timestamp: '2026-01-10T10:00:00Z', galactic: '3312-01-10 10:00 UTC', event: 'FSDJump' }],
      }),
    )
    let resolveStart!: () => void
    mockStartExpedition.mockImplementationOnce(
      () => new Promise<void>((resolve) => { resolveStart = resolve }),
    )

    const onClose = vi.fn()
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)
    try {
      await act(async () => {
        root.render(
          createElement(StartExpeditionModal, {
            expeditionId: 5,
            modesList: [modes.Mode.createFrom({ id: 1, name: 'Travel', color: '#FF7100' })],
            initialModeId: 1,
            onClose,
          }),
        )
        await Promise.resolve()
      })
      const buttons = () => Array.from(container.querySelectorAll('button'))
      const findButton = (label: string) =>
        buttons().find((button) => button.textContent?.includes(label)) as HTMLButtonElement

      act(() => findButton('CONFIRM AND ENGAGE').click())
      expect(mockStartExpedition).toHaveBeenCalledWith(5, '2026-01-10T10:00:00Z', 1)
      expect(findButton('ENGAGING...').disabled).toBe(true)
      expect(findButton('CANCEL').disabled).toBe(true)
      expect(buttons()[0].disabled).toBe(true)

      await act(async () => { resolveStart(); await Promise.resolve() })
      expect(onClose).toHaveBeenCalledTimes(1)
    } finally {
      act(() => root.unmount())
      container.remove()
    }

    let rejectCandidates!: (reason: Error) => void
    mockGetStartCandidates.mockImplementationOnce(
      () => new Promise<app.StartCandidates>((_, reject) => { rejectCandidates = reject }),
    )
    const unmountContainer = document.createElement('div')
    document.body.appendChild(unmountContainer)
    const unmountRoot = createRoot(unmountContainer)
    act(() => unmountRoot.render(createElement(StartExpeditionModal, { expeditionId: 6, onClose: () => {} })))
    act(() => unmountRoot.unmount())
    unmountContainer.remove()
    await act(async () => { rejectCandidates(new Error('aborted')); await Promise.resolve() })
  })
})
