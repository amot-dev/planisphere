// @vitest-environment happy-dom
import { act, createElement } from 'react'
import { describe, expect, it, vi } from 'vitest'
import { app } from '../../../api/models'
import { makeCommander, makeMode } from '../../../test/fixtures'
import { mountTestRoot, setFormControlValue, type MountedTestRoot } from '../../../test/render'
import { installWailsAppMock } from '../../../test/wailsMock'
import { StartExpeditionModal } from './StartExpeditionModal'

describe('StartExpeditionModal', () => {
  it('disables close and action buttons during StartExpedition, hides scope picker for <=1 commander, and ignores candidate settlement after unmount', async () => {
    let resolveStart!: () => void
    let rejectCandidates!: (reason: Error) => void
    const { spies } = installWailsAppMock({
      GetStartCandidates: async () =>
        app.StartCandidates.createFrom({
          systemName: 'Sol',
          visits: [
            {
              uid: 'v1',
              timestamp: '2026-01-10T10:00:00Z',
              galactic: '3312-01-10 10:00 UTC',
              event: 'FSDJump',
            },
          ],
        }),
      StartExpedition: () =>
        new Promise<void>((resolve) => {
          resolveStart = resolve
        }),
      StartExpeditionScoped: async () => {},
    })

    const onClose = vi.fn()
    let mounted!: MountedTestRoot
    await act(async () => {
      mounted = mountTestRoot(
        createElement(StartExpeditionModal, {
          expeditionId: 5,
          modesList: [
            makeMode({ id: 9, name: 'Custom Cruise', color: '#00D2FF', isDefault: false }),
            makeMode({ id: 2, name: 'Standard Transit', color: '#FF7100', isDefault: true }),
          ],
          defaultModeId: 2,
          commanders: [
            makeCommander({ id: 1, profileKey: 'live:f1:era:0', name: 'Solo', enabled: true }),
          ],
          onClose,
        }),
      )
      await Promise.resolve()
    })
    expect(mounted.container.textContent).not.toContain('Save-Slot Scope')

    act(() => mounted.findButtonByText('CONFIRM AND ENGAGE').click())
    expect(spies.StartExpedition).toHaveBeenCalledWith(5, '2026-01-10T10:00:00Z', 2)
    expect(spies.StartExpeditionScoped).not.toHaveBeenCalled()
    expect(mounted.findButtonByText('ENGAGING...').disabled).toBe(true)
    expect(mounted.findButtonByText('CANCEL').disabled).toBe(true)
    const headerCloseButton = mounted.container.querySelector('button') as HTMLButtonElement
    expect(headerCloseButton.disabled).toBe(true)

    await act(async () => {
      resolveStart()
      await Promise.resolve()
    })
    expect(onClose).toHaveBeenCalledTimes(1)
    mounted.unmount()

    spies.GetStartCandidates.mockImplementationOnce(
      () =>
        new Promise<app.StartCandidates>((_, reject) => {
          rejectCandidates = reject
        }),
    )
    const unmountedModal = mountTestRoot(
      createElement(StartExpeditionModal, { expeditionId: 6, onClose: () => {} }),
    )
    unmountedModal.unmount()
    await act(async () => {
      rejectCandidates(new Error('aborted'))
      await Promise.resolve()
    })
  })

  it('auto-preselects visit.profileId when multiple commanders exist and calls StartExpeditionScoped', async () => {
    const { spies } = installWailsAppMock({
      GetStartCandidates: async () =>
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
      StartExpeditionScoped: async () => {},
    })

    const commanders = [
      makeCommander({ id: 1, profileKey: 'live:f1:era:0', name: 'MainCmdr', fid: 'F1', enabled: true, eventCount: 500 }),
      makeCommander({ id: 2, profileKey: 'live:f2:era:0', name: 'AltExplorer', fid: 'F2', enabled: true, eventCount: 120 }),
      makeCommander({ id: 3, profileKey: 'live:f3:era:0', name: 'BubbleTrader', fid: 'F3', enabled: true, eventCount: 40 }),
    ]

    const onClose = vi.fn()
    let mounted!: MountedTestRoot
    await act(async () => {
      mounted = mountTestRoot(
        createElement(StartExpeditionModal, {
          expeditionId: 8,
          modesList: [makeMode({ id: 3, name: 'Exploration', color: '#00B3FF' })],
          initialModeId: 3,
          commanders,
          onClose,
        }),
      )
      await Promise.resolve()
    })

    expect(mounted.container.textContent).toContain('Save-Slot Scope')
    const visitSelect = mounted.container.querySelector(
      'select[aria-label="Recorded visit to Waypoint #1"]',
    ) as HTMLSelectElement
    expect(visitSelect.textContent).toContain('(AltExplorer)')

    const checkboxes = () =>
      Array.from(mounted.container.querySelectorAll('input[type="checkbox"]')) as HTMLInputElement[]
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

    await act(async () => {
      mounted.findButtonByText('CONFIRM AND ENGAGE').click()
      await Promise.resolve()
    })
    expect(spies.StartExpeditionScoped).not.toHaveBeenCalled()
    expect(mounted.container.textContent).toContain(
      'Selected Waypoint #1 visit does not belong to any selected commander save slot.',
    )

    // Switching to All Enabled, picking AltExplorer's visit, then switching back to Selected re-syncs to a matching visit when profile 1 is re-checked.
    act(() => checkboxes()[0].click())
    act(() => checkboxes()[2].click())
    act(() => mounted.findButtonByText('All Enabled Save Slots').click())
    act(() => {
      setFormControlValue(visitSelect, '2026-02-01T12:00:00Z')
    })
    expect(visitSelect.value).toBe('2026-02-01T12:00:00Z')
    act(() => mounted.findButtonByText('Selected Save Slot(s)').click())
    expect(visitSelect.value).toBe('2026-01-15T09:00:00Z')

    await act(async () => {
      mounted.findButtonByText('CONFIRM AND ENGAGE').click()
      await Promise.resolve()
    })
    expect(spies.StartExpeditionScoped).toHaveBeenCalledWith(
      8,
      '2026-01-15T09:00:00Z',
      3,
      'selected',
      [1],
    )
    expect(onClose).toHaveBeenCalledTimes(1)
  })
})
