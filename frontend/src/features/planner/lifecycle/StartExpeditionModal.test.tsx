// @vitest-environment happy-dom
import { act, createElement } from 'react'
import { describe, expect, it, vi } from 'vitest'
import { app } from '../../../api/models'
import { makeCommander, makeMode } from '../../../test/fixtures'
import { mountTestRoot, type MountedTestRoot } from '../../../test/render'
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
    expect(mounted.container.textContent).not.toContain('Commander Save Slot')

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

  it('defaults to latest commander when multiple commanders exist, filters Waypoint #1 visits per save slot, and calls StartExpeditionScoped', async () => {
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
      makeCommander({
        id: 1,
        profileKey: 'live:f1:era:0',
        name: 'MainCmdr',
        fid: 'F1',
        enabled: true,
        lastSeen: '2026-01-15T09:00:00Z',
        eventCount: 500,
      }),
      makeCommander({
        id: 2,
        profileKey: 'live:f2:era:0',
        name: 'AltExplorer',
        fid: 'F2',
        enabled: true,
        lastSeen: '2026-02-01T12:00:00Z',
        eventCount: 120,
      }),
      makeCommander({
        id: 3,
        profileKey: 'live:f3:era:0',
        name: 'BubbleTrader',
        fid: 'F3',
        enabled: true,
        lastSeen: '2026-01-05T08:00:00Z',
        eventCount: 40,
      }),
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

    expect(mounted.container.textContent).toContain('Commander Save Slot')
    const radios = () =>
      Array.from(mounted.container.querySelectorAll('input[type="radio"]')) as HTMLInputElement[]
    expect(radios()[0].checked).toBe(false)
    expect(radios()[1].checked).toBe(true)
    expect(radios()[2].checked).toBe(false)

    const getVisitSelect = () =>
      mounted.container.querySelector(
        'select[aria-label="Recorded visit to Waypoint #1"]',
      ) as HTMLSelectElement | null
    expect(getVisitSelect()?.value).toBe('2026-02-01T12:00:00Z')
    expect(getVisitSelect()?.textContent).toContain('(AltExplorer)')
    expect(getVisitSelect()?.textContent).not.toContain('(MainCmdr)')

    // Switching to BubbleTrader (zero Waypoint #1 visits) clears selection and disables engage.
    act(() => radios()[2].click())
    expect(radios()[2].checked).toBe(true)
    expect(getVisitSelect()).toBeNull()
    expect(mounted.container.textContent).toContain(
      'No historical visits to Shinrarta Dezhra found for CMDR BubbleTrader yet.',
    )
    expect(mounted.findButtonByText('CONFIRM AND ENGAGE').disabled).toBe(true)

    // Switching to MainCmdr selects MainCmdr's recorded visit and submits StartExpeditionScoped.
    act(() => radios()[0].click())
    expect(radios()[0].checked).toBe(true)
    expect(getVisitSelect()?.value).toBe('2026-01-15T09:00:00Z')

    await act(async () => {
      mounted.findButtonByText('CONFIRM AND ENGAGE').click()
      await Promise.resolve()
    })
    expect(spies.StartExpeditionScoped).toHaveBeenCalledWith(
      8,
      '2026-01-15T09:00:00Z',
      3,
      1,
    )
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('syncs selected commander and visits on mounted prop updates while preserving valid user overrides until commander removal or expedition change', async () => {
    const { spies } = installWailsAppMock({
      GetStartCandidates: async (requestedExpeditionId: number) =>
        requestedExpeditionId === 8
          ? app.StartCandidates.createFrom({
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
            })
          : app.StartCandidates.createFrom({
              systemName: 'Colonia',
              visits: [
                {
                  uid: 'u3',
                  timestamp: '2026-03-01T18:00:00Z',
                  galactic: '3312-03-01 18:00 UTC',
                  event: 'FSDJump',
                  profileId: 2,
                  commander: 'AltExplorer',
                },
              ],
            }),
      StartExpeditionScoped: async () => {},
    })

    const cmdrMain = makeCommander({
      id: 1,
      profileKey: 'live:f1:era:0',
      name: 'MainCmdr',
      fid: 'F1',
      enabled: true,
      lastSeen: '2026-01-15T09:00:00Z',
    })
    const cmdrAlt = makeCommander({
      id: 2,
      profileKey: 'live:f2:era:0',
      name: 'AltExplorer',
      fid: 'F2',
      enabled: true,
      lastSeen: '2026-02-01T12:00:00Z',
    })
    const cmdrThird = makeCommander({
      id: 3,
      profileKey: 'live:f3:era:0',
      name: 'BubbleTrader',
      fid: 'F3',
      enabled: true,
      lastSeen: '2026-01-05T08:00:00Z',
    })
    const modeList = [makeMode({ id: 3, name: 'Exploration', color: '#00B3FF' })]
    const onClose = vi.fn()

    let mounted!: MountedTestRoot
    await act(async () => {
      mounted = mountTestRoot(
        createElement(StartExpeditionModal, {
          expeditionId: 8,
          modesList: modeList,
          initialModeId: 3,
          commanders: [cmdrMain, cmdrAlt, cmdrThird],
          onClose,
        }),
      )
      await Promise.resolve()
    })

    const radios = () =>
      Array.from(mounted.container.querySelectorAll('input[type="radio"]')) as HTMLInputElement[]
    const getVisitSelect = () =>
      mounted.container.querySelector(
        'select[aria-label="Recorded visit to Waypoint #1"]',
      ) as HTMLSelectElement | null

    // Initially defaults to latest commander AltExplorer (id=2).
    expect(radios()[1].checked).toBe(true)
    expect(getVisitSelect()?.value).toBe('2026-02-01T12:00:00Z')

    // Before user touches the picker, updating initialCommanderProfileId while mounted re-syncs both profile and visit.
    await act(async () => {
      mounted.rerender(
        createElement(StartExpeditionModal, {
          expeditionId: 8,
          modesList: modeList,
          initialModeId: 3,
          commanders: [cmdrMain, cmdrAlt, cmdrThird],
          initialCommanderProfileId: 1,
          onClose,
        }),
      )
      await Promise.resolve()
    })
    expect(radios()[0].checked).toBe(true)
    expect(getVisitSelect()?.value).toBe('2026-01-15T09:00:00Z')

    // User manually selects BubbleTrader (id=3); subsequent prop refresh on same expedition preserves user choice.
    act(() => radios()[2].click())
    expect(radios()[2].checked).toBe(true)
    expect(getVisitSelect()).toBeNull()

    await act(async () => {
      mounted.rerender(
        createElement(StartExpeditionModal, {
          expeditionId: 8,
          modesList: modeList,
          initialModeId: 3,
          commanders: [cmdrMain, cmdrAlt, cmdrThird],
          initialCommanderProfileId: 2,
          onClose,
        }),
      )
      await Promise.resolve()
    })
    expect(radios()[2].checked).toBe(true)

    // Removing the user-selected commander (id=3) from commanders forces re-resolution to initialCommanderProfileId (id=1).
    await act(async () => {
      mounted.rerender(
        createElement(StartExpeditionModal, {
          expeditionId: 8,
          modesList: modeList,
          initialModeId: 3,
          commanders: [cmdrMain, cmdrAlt],
          initialCommanderProfileId: 1,
          onClose,
        }),
      )
      await Promise.resolve()
    })
    expect(radios()[0].checked).toBe(true)
    expect(getVisitSelect()?.value).toBe('2026-01-15T09:00:00Z')

    // User manually selects MainCmdr (id=1), then expeditionId changes to 9 with initialCommanderProfileId=2; resets touch state and loads Colonia.
    act(() => radios()[0].click())
    expect(radios()[0].checked).toBe(true)

    await act(async () => {
      mounted.rerender(
        createElement(StartExpeditionModal, {
          expeditionId: 9,
          modesList: modeList,
          initialModeId: 3,
          commanders: [cmdrMain, cmdrAlt],
          initialCommanderProfileId: 2,
          onClose,
        }),
      )
      await Promise.resolve()
    })
    expect(radios()[1].checked).toBe(true)
    expect(getVisitSelect()?.value).toBe('2026-03-01T18:00:00Z')

    await act(async () => {
      mounted.findButtonByText('CONFIRM AND ENGAGE').click()
      await Promise.resolve()
    })
    expect(spies.StartExpeditionScoped).toHaveBeenCalledWith(9, '2026-03-01T18:00:00Z', 3, 2)
  })
})
