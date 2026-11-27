// @vitest-environment happy-dom
import { act, createElement } from 'react'
import { describe, expect, it, vi } from 'vitest'
import { expedition } from '../../../api/models'
import { mountTestRoot, setFormControlValue, type MountedTestRoot } from '../../../test/render'
import { installWailsAppMock } from '../../../test/wailsMock'
import { WaypointSearchModal, type WaypointSearchModalProps } from './WaypointSearchModal'

function mountSearchModal(props: Partial<WaypointSearchModalProps> = {}): MountedTestRoot {
  return mountTestRoot(
    createElement(WaypointSearchModal, {
      activeExpeditionId: 10,
      onClose: vi.fn(),
      onNavigateToWaypoint: vi.fn(),
      onSelectExpedition: vi.fn(),
      ...props,
    }),
  )
}

describe('WaypointSearchModal', () => {
  it('executes immediately for non-empty initialQuery, navigates single-stop and hub cards, and renders truncation footer', async () => {
    const { spies } = installWailsAppMock({
      SearchWaypoints: async () =>
        expedition.SearchResponse.createFrom({
          totalMatches: 54,
          results: [
            {
              expeditionId: 10,
              expeditionName: 'Colonia Loop',
              expeditionStatus: 'active',
              systemName: 'Sagittarius A*',
              poiName: 'Supermassive Black Hole',
              relevanceTier: 1,
              stops: [
                {
                  rowId: 201,
                  number: 4,
                  poiName: 'Supermassive Black Hole',
                  visitedAt: '2026-01-18T10:00:00Z',
                  visitedAtGalactic: '3312-01-18 10:00 UTC',
                },
              ],
            },
            {
              expeditionId: 20,
              expeditionName: 'Carrier Hub Run',
              expeditionStatus: 'completed',
              systemName: 'Colonia',
              poiName: 'Jaques Station / Carrier Staging',
              relevanceTier: 2,
              stops: [
                {
                  rowId: 301,
                  number: 2,
                  poiName: 'Jaques Station',
                  visitedAt: '2026-01-01T08:00:00Z',
                  visitedAtGalactic: '3312-01-01 08:00 UTC',
                },
                {
                  rowId: 309,
                  number: 11,
                  poiName: 'Carrier Staging',
                  visitedAt: '',
                  visitedAtGalactic: '',
                },
              ],
            },
          ],
        }),
    })

    const onNavigateToWaypoint = vi.fn()
    const modalView = mountSearchModal({
      initialQuery: 'Sagittarius A*',
      activeExpeditionId: 10,
      onNavigateToWaypoint,
    })

    // Immediate 0ms search without waiting for debounce.
    expect(spies.SearchWaypoints).toHaveBeenCalledTimes(1)
    expect(spies.SearchWaypoints).toHaveBeenCalledWith('Sagittarius A*')

    await act(async () => {
      await Promise.resolve()
    })

    expect(modalView.container.textContent).toContain('Colonia Loop')
    expect(modalView.container.textContent).toContain('WP #04')
    expect(modalView.container.textContent).toContain('Visited 3312-01-18 10:00 UTC')
    expect(modalView.container.textContent).toContain('CURRENT')
    expect(modalView.container.textContent).toContain('2 WAYPOINTS')
    expect(modalView.container.textContent).toContain(
      'Showing 2 of 54 matches - refine query to narrow',
    )

    // Ensure hub card does not nest a button inside another button.
    expect(modalView.container.querySelector('button button')).toBeNull()

    const resultOptions = Array.from(modalView.container.querySelectorAll('[role="option"]'))
    expect(resultOptions).toHaveLength(2)

    // Clicking single-stop card navigates to its stop.
    act(() => {
      ;(resultOptions[0] as HTMLElement).click()
    })
    expect(onNavigateToWaypoint).toHaveBeenLastCalledWith(10, 201)

    // Clicking outer hub card navigates to its first stop.
    act(() => {
      ;(resultOptions[1] as HTMLElement).click()
    })
    expect(onNavigateToWaypoint).toHaveBeenLastCalledWith(20, 301)

    // Clicking a specific stop chip navigates to that chip without bubbling to outer card.
    const secondStopChip = modalView.findButtonByText('WP #11 · Unvisited')
    act(() => {
      secondStopChip.click()
    })
    expect(onNavigateToWaypoint).toHaveBeenCalledTimes(3)
    expect(onNavigateToWaypoint).toHaveBeenLastCalledWith(20, 309)

    // ArrowDown + Enter selects second result and navigates to its first stop.
    const searchInput = modalView.container.querySelector('input') as HTMLInputElement
    act(() => {
      searchInput.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true }))
    })
    act(() => {
      searchInput.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))
    })
    expect(onNavigateToWaypoint).toHaveBeenCalledTimes(4)
    expect(onNavigateToWaypoint).toHaveBeenLastCalledWith(20, 301)
  })

  it('renders NON-WAYPOINT VISITS with 3-row default collapse, Show all / Show fewer toggle, and expedition click', async () => {
    installWailsAppMock({
      SearchWaypoints: async () =>
        expedition.SearchResponse.createFrom({
          totalMatches: 0,
          results: [],
          nonWaypointVisits: {
            systemName: 'Shinrarta Dezhra',
            totalCount: 5,
            earliestGalactic: '3309-04-10 09:00 UTC',
            visits: [
              {
                timestamp: '2026-02-05T12:00:00Z',
                galactic: '3312-02-05 12:00 UTC',
                expeditionId: 77,
                expeditionName: 'Bubble Refit',
                expeditionStatus: 'completed',
              },
              {
                timestamp: '2026-02-04T12:00:00Z',
                galactic: '3312-02-04 12:00 UTC',
              },
              {
                timestamp: '2026-02-03T12:00:00Z',
                galactic: '3312-02-03 12:00 UTC',
              },
              {
                timestamp: '2026-02-02T12:00:00Z',
                galactic: '3312-02-02 12:00 UTC',
              },
            ],
          },
        }),
    })

    const onSelectExpedition = vi.fn()
    const onClose = vi.fn()
    const modalView = mountSearchModal({
      initialQuery: 'Shinrarta Dezhra',
      onSelectExpedition,
      onClose,
    })

    await act(async () => {
      await Promise.resolve()
    })

    expect(modalView.container.textContent).toContain('NON-WAYPOINT VISITS')
    expect(modalView.container.textContent).toContain('5 VISITS')
    expect(modalView.container.textContent).toContain('3312-02-05 12:00 UTC')
    expect(modalView.container.textContent).toContain('Outside tracked expeditions')
    expect(modalView.container.textContent).not.toContain('3312-02-02 12:00 UTC')

    const showAllButton = modalView.findButtonByText(
      '+2 earlier visits (first: 3309-04-10 09:00 UTC) · Show all',
    )
    act(() => {
      showAllButton.click()
    })
    expect(modalView.container.textContent).toContain('3312-02-02 12:00 UTC')
    expect(modalView.container.textContent).toContain(
      'Showing 4 most recent of 5 visits (earliest: 3309-04-10 09:00 UTC)',
    )

    const showFewerButton = modalView.findButtonByText('Show fewer')
    act(() => {
      showFewerButton.click()
    })
    expect(modalView.container.textContent).not.toContain('3312-02-02 12:00 UTC')

    const expeditionButton = modalView.findButtonByText(
      'During expedition: Bubble Refit [COMPLETED]',
    )
    act(() => {
      expeditionButton.click()
    })
    expect(onSelectExpedition).toHaveBeenCalledWith(77)
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('ignores stale out-of-order search responses and dismisses on Escape', async () => {
    vi.useFakeTimers()

    let resolveFirstRequest!: (response: expedition.SearchResponse) => void
    let resolveSecondRequest!: (response: expedition.SearchResponse) => void

    const { spies } = installWailsAppMock({
      SearchWaypoints: async () => expedition.SearchResponse.createFrom({}),
    })
    spies.SearchWaypoints
      .mockImplementationOnce(
        () =>
          new Promise<expedition.SearchResponse>((resolve) => {
            resolveFirstRequest = resolve
          }),
      )
      .mockImplementationOnce(
        () =>
          new Promise<expedition.SearchResponse>((resolve) => {
            resolveSecondRequest = resolve
          }),
      )

    const onClose = vi.fn()
    const modalView = mountSearchModal({ initialQuery: '', onClose })

    expect(spies.SearchWaypoints).not.toHaveBeenCalled()
    const searchInput = modalView.container.querySelector('input') as HTMLInputElement

    act(() => {
      setFormControlValue(searchInput, 'Col')
    })
    expect(spies.SearchWaypoints).not.toHaveBeenCalled()

    act(() => {
      vi.advanceTimersByTime(120)
    })
    expect(spies.SearchWaypoints).toHaveBeenNthCalledWith(1, 'Col')

    act(() => {
      setFormControlValue(searchInput, 'Colonia')
    })
    act(() => {
      vi.advanceTimersByTime(120)
    })
    expect(spies.SearchWaypoints).toHaveBeenNthCalledWith(2, 'Colonia')

    // Newer query resolves first.
    await act(async () => {
      resolveSecondRequest(
        expedition.SearchResponse.createFrom({
          totalMatches: 1,
          results: [
            {
              expeditionId: 10,
              expeditionName: 'Newer Result Expedition',
              expeditionStatus: 'active',
              systemName: 'Colonia',
              poiName: '',
              relevanceTier: 1,
              stops: [{ rowId: 502, number: 2, poiName: '', visitedAt: '', visitedAtGalactic: '' }],
            },
          ],
        }),
      )
      await Promise.resolve()
    })
    expect(modalView.container.textContent).toContain('Newer Result Expedition')

    // Older request resolves afterward and must not clobber newer results.
    await act(async () => {
      resolveFirstRequest(
        expedition.SearchResponse.createFrom({
          totalMatches: 1,
          results: [
            {
              expeditionId: 99,
              expeditionName: 'Stale Result Expedition',
              expeditionStatus: 'planning',
              systemName: 'Col 285 Sector',
              poiName: '',
              relevanceTier: 2,
              stops: [{ rowId: 501, number: 1, poiName: '', visitedAt: '', visitedAtGalactic: '' }],
            },
          ],
        }),
      )
      await Promise.resolve()
    })
    expect(modalView.container.textContent).toContain('Newer Result Expedition')
    expect(modalView.container.textContent).not.toContain('Stale Result Expedition')

    // Escape dismisses exactly once whether dispatched on the input or globally on window.
    act(() => {
      searchInput.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    })
    expect(onClose).toHaveBeenCalledTimes(1)

    act(() => {
      searchInput.blur()
      window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    })
    expect(onClose).toHaveBeenCalledTimes(2)
  })

  it('blocks Enter navigation during debounce or in-flight edits and clears prior results when a search rejects', async () => {
    vi.useFakeTimers()

    let rejectSecondRequest!: (error: Error) => void
    const { spies } = installWailsAppMock({
      SearchWaypoints: async () => expedition.SearchResponse.createFrom({}),
    })
    spies.SearchWaypoints
      .mockResolvedValueOnce(
        expedition.SearchResponse.createFrom({
          totalMatches: 1,
          results: [
            {
              expeditionId: 10,
              expeditionName: 'Prior Query Expedition',
              expeditionStatus: 'active',
              systemName: 'Sol',
              poiName: '',
              relevanceTier: 1,
              stops: [{ rowId: 601, number: 1, poiName: '', visitedAt: '', visitedAtGalactic: '' }],
            },
          ],
        }),
      )
      .mockImplementationOnce(
        () =>
          new Promise<expedition.SearchResponse>((_resolve, reject) => {
            rejectSecondRequest = reject
          }),
      )

    const onNavigateToWaypoint = vi.fn()
    const modalView = mountSearchModal({
      initialQuery: 'Sol',
      onNavigateToWaypoint,
    })

    await act(async () => {
      await Promise.resolve()
    })
    expect(modalView.container.textContent).toContain('Prior Query Expedition')

    // Typing a new query starts the 120ms debounce window; Enter must not jump to the stale prior result.
    const searchInput = modalView.container.querySelector('input') as HTMLInputElement
    act(() => {
      setFormControlValue(searchInput, 'Colonia')
    })
    act(() => {
      searchInput.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))
    })
    expect(onNavigateToWaypoint).not.toHaveBeenCalled()

    // Advance past debounce so the replacement request is in flight; Enter must still be ignored.
    act(() => {
      vi.advanceTimersByTime(120)
    })
    expect(spies.SearchWaypoints).toHaveBeenNthCalledWith(2, 'Colonia')
    act(() => {
      searchInput.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))
    })
    expect(onNavigateToWaypoint).not.toHaveBeenCalled()

    // When the replacement request rejects, stale prior results are cleared beneath the error banner.
    await act(async () => {
      rejectSecondRequest(new Error('search unavailable'))
      await Promise.resolve()
    })
    expect(modalView.container.textContent).toContain('search unavailable')
    expect(modalView.container.textContent).not.toContain('Prior Query Expedition')
    act(() => {
      searchInput.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))
    })
    expect(onNavigateToWaypoint).not.toHaveBeenCalled()
  })
})
