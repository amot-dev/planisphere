// @vitest-environment happy-dom
import { act, createElement } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { app, modes, replay } from '../../../api/models'

const mockGetStartCandidates = vi.fn()
const mockStartExpedition = vi.fn()
const mockStartExpeditionScoped = vi.fn()

vi.mock('../../../api/backend', () => ({
  GetStartCandidates: (...args: unknown[]) => mockGetStartCandidates(...args),
  StartExpedition: (...args: unknown[]) => mockStartExpedition(...args),
  StartExpeditionScoped: (...args: unknown[]) => mockStartExpeditionScoped(...args),
}))

import { StartExpeditionModal } from './StartExpeditionModal'

;(globalThis as unknown as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true

describe('StartExpeditionModal', () => {
  afterEach(() => vi.clearAllMocks())

  it('disables close and action buttons during StartExpedition, hides scope picker for <=1 commander, and ignores candidate settlement after unmount', async () => {
    mockGetStartCandidates.mockResolvedValueOnce(
      app.StartCandidates.createFrom({
        systemName: 'Sol',
        visits: [{ uid: 'v1', timestamp: '2026-01-10T10:00:00Z', galactic: '3312-01-10 10:00 UTC', event: 'FSDJump' }],
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
            commanders: [replay.Commander.createFrom({ id: 1, profileKey: 'live:f1:era:0', name: 'Solo', enabled: true })],
            onClose,
          }),
        )
        await Promise.resolve()
      })
      expect(container.textContent).not.toContain('Save-Slot Scope')

      const buttons = () => Array.from(container.querySelectorAll('button'))
      const findButton = (label: string) =>
        buttons().find((button) => button.textContent?.includes(label)) as HTMLButtonElement

      act(() => findButton('CONFIRM AND ENGAGE').click())
      expect(mockStartExpedition).toHaveBeenCalledWith(5, '2026-01-10T10:00:00Z', 1)
      expect(mockStartExpeditionScoped).not.toHaveBeenCalled()
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

  it('auto-preselects visit.profileId when multiple commanders exist and calls StartExpeditionScoped', async () => {
    mockGetStartCandidates.mockResolvedValueOnce(
      app.StartCandidates.createFrom({
        systemName: 'Shinrarta Dezhra',
        visits: [
          {
            uid: 'u1',
            timestamp: '2026-02-01T12:00:00Z',
            galactic: '3312-02-01 12:00 UTC',
            event: 'Location',
            profileId: 2,
            commander: 'AltExplorer',
          },
          {
            uid: 'u2',
            timestamp: '2026-01-15T09:00:00Z',
            galactic: '3312-01-15 09:00 UTC',
            event: 'FSDJump',
            profileId: 1,
            commander: 'MainCmdr',
          },
        ],
      }),
    )
    mockStartExpeditionScoped.mockResolvedValueOnce(undefined)

    const commanders = [
      replay.Commander.createFrom({ id: 1, profileKey: 'live:f1:era:0', name: 'MainCmdr', fid: 'F1', enabled: true, eventCount: 500 }),
      replay.Commander.createFrom({ id: 2, profileKey: 'live:f2:era:0', name: 'AltExplorer', fid: 'F2', enabled: true, eventCount: 120 }),
      replay.Commander.createFrom({ id: 3, profileKey: 'live:f3:era:0', name: 'BubbleTrader', fid: 'F3', enabled: true, eventCount: 40 }),
    ]

    const onClose = vi.fn()
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)
    try {
      await act(async () => {
        root.render(
          createElement(StartExpeditionModal, {
            expeditionId: 8,
            modesList: [modes.Mode.createFrom({ id: 3, name: 'Exploration', color: '#00B3FF' })],
            initialModeId: 3,
            commanders,
            onClose,
          }),
        )
        await Promise.resolve()
      })

      expect(container.textContent).toContain('Save-Slot Scope')
      const visitSelect = container.querySelector(
        'select[aria-label="Recorded visit to Waypoint #1"]',
      ) as HTMLSelectElement
      expect(visitSelect.textContent).toContain('(AltExplorer)')

      const checkboxes = () =>
        Array.from(container.querySelectorAll('input[type="checkbox"]')) as HTMLInputElement[]
      expect(checkboxes()[0].checked).toBe(false)
      expect(checkboxes()[1].checked).toBe(true)
      expect(checkboxes()[2].checked).toBe(false)

      // Selecting MainCmdr and unchecking AltExplorer automatically re-syncs visitSelect to MainCmdr's visit.
      act(() => checkboxes()[0].click())
      act(() => checkboxes()[1].click())
      expect(checkboxes()[0].checked).toBe(true)
      expect(checkboxes()[1].checked).toBe(false)
      expect(visitSelect.value).toBe('2026-01-15T09:00:00Z')

      // Switching scope solely to BubbleTrader (profileId 3, zero Waypoint #1 visits) blocks engage with an error.
      act(() => checkboxes()[2].click())
      act(() => checkboxes()[0].click())
      expect(checkboxes()[0].checked).toBe(false)
      expect(checkboxes()[2].checked).toBe(true)
      expect(visitSelect.value).toBe('2026-01-15T09:00:00Z')

      const findButton = (label: string) =>
        Array.from(container.querySelectorAll('button')).find((button) =>
          button.textContent?.includes(label),
        ) as HTMLButtonElement
      await act(async () => {
        findButton('CONFIRM AND ENGAGE').click()
        await Promise.resolve()
      })
      expect(mockStartExpeditionScoped).not.toHaveBeenCalled()
      expect(container.textContent).toContain(
        'Selected Waypoint #1 visit does not belong to any selected commander save slot.',
      )

      // Switching to All Enabled, picking AltExplorer's visit, then switching back to Selected re-syncs to a matching visit when profile 1 is re-checked.
      act(() => checkboxes()[0].click())
      act(() => checkboxes()[2].click())
      act(() => findButton('All Enabled Save Slots').click())
      act(() => {
        const descriptor = Object.getOwnPropertyDescriptor(HTMLSelectElement.prototype, 'value')
        descriptor?.set?.call(visitSelect, '2026-02-01T12:00:00Z')
        visitSelect.dispatchEvent(new Event('change', { bubbles: true }))
      })
      expect(visitSelect.value).toBe('2026-02-01T12:00:00Z')
      act(() => findButton('Selected Save Slot(s)').click())
      expect(visitSelect.value).toBe('2026-01-15T09:00:00Z')

      await act(async () => {
        findButton('CONFIRM AND ENGAGE').click()
        await Promise.resolve()
      })
      expect(mockStartExpeditionScoped).toHaveBeenCalledWith(
        8,
        '2026-01-15T09:00:00Z',
        3,
        'selected',
        [1],
      )
      expect(onClose).toHaveBeenCalledTimes(1)
    } finally {
      act(() => root.unmount())
      container.remove()
    }
  })
})
