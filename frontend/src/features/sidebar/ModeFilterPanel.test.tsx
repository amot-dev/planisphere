// @vitest-environment happy-dom
import { act, createElement } from 'react'
import { describe, expect, it, vi } from 'vitest'
import { app, stats } from '../../api/models'
import { makeMode } from '../../test/fixtures'
import { mountTestRoot } from '../../test/render'
import { ModeFilterPanel } from './ModeFilterPanel'

describe('ModeFilterPanel', () => {
  const modesState = app.ModesState.createFrom({
    modes: [
      makeMode({ id: 1, name: 'Travel', color: '#FF7100' }),
      makeMode({ id: 2, name: 'Survey', color: '#00CCFF' }),
    ],
  })

  it('cycles per-mode tri-state selections, resets all modes to full inclusion, and invokes sidebar collapse', () => {
    const onFilterChange = vi.fn()
    const onToggleCollapse = vi.fn()

    let currentFilter = stats.Filter.createFrom({ enabled: false, modes: [] })
    const mounted = mountTestRoot(
      createElement(ModeFilterPanel, {
        modesState,
        filter: currentFilter,
        onFilterChange: (nextFilter: stats.Filter) => {
          currentFilter = nextFilter
          onFilterChange(nextFilter)
        },
        onToggleCollapse,
      }),
    )

    // Reset button is hidden while filter.enabled is false.
    expect(mounted.queryButtonByText('[Reset]')).toBeNull()

    const collapseButton = mounted.container.querySelector(
      'button[title="Collapse Telemetry Sidebar"]',
    ) as HTMLButtonElement
    act(() => collapseButton.click())
    expect(onToggleCollapse).toHaveBeenCalledTimes(1)

    // Clicking Travel cycles Full -> Partial (Scans Only: travel=false, discovery=true, enabled=true).
    act(() => {
      mounted.findButtonByText('Travel').click()
    })
    expect(currentFilter.enabled).toBe(true)
    expect(currentFilter.modes).toEqual([
      expect.objectContaining({ modeId: 1, travel: false, discovery: true }),
      expect.objectContaining({ modeId: 2, travel: true, discovery: true }),
    ])

    // Rerendering with Partial state shows [Reset]; clicking Travel again cycles Partial -> Off (travel=false, discovery=false).
    mounted.rerender(
      createElement(ModeFilterPanel, {
        modesState,
        filter: currentFilter,
        onFilterChange: (nextFilter: stats.Filter) => {
          currentFilter = nextFilter
          onFilterChange(nextFilter)
        },
      }),
    )
    expect(mounted.findButtonByText('[Reset]')).toBeTruthy()

    act(() => {
      mounted.findButtonByText('Travel').click()
    })
    expect(currentFilter.enabled).toBe(true)
    expect(currentFilter.modes).toEqual([
      expect.objectContaining({ modeId: 1, travel: false, discovery: false }),
      expect.objectContaining({ modeId: 2, travel: true, discovery: true }),
    ])

    // Clicking Travel from Off cycles back to Full and disables the master filter when all modes are full.
    mounted.rerender(
      createElement(ModeFilterPanel, {
        modesState,
        filter: currentFilter,
        onFilterChange: (nextFilter: stats.Filter) => {
          currentFilter = nextFilter
          onFilterChange(nextFilter)
        },
      }),
    )
    act(() => {
      mounted.findButtonByText('Travel').click()
    })
    expect(currentFilter.enabled).toBe(false)
    expect(currentFilter.modes).toEqual([
      expect.objectContaining({ modeId: 1, travel: true, discovery: true }),
      expect.objectContaining({ modeId: 2, travel: true, discovery: true }),
    ])

    // Clicking [Reset] from an active filter restores every mode to full inclusion and sets enabled=false.
    const activeSubsetFilter = stats.Filter.createFrom({
      enabled: true,
      modes: [
        { modeId: 1, travel: false, discovery: false },
        { modeId: 2, travel: false, discovery: true },
      ],
    })
    mounted.rerender(
      createElement(ModeFilterPanel, {
        modesState,
        filter: activeSubsetFilter,
        onFilterChange: (nextFilter: stats.Filter) => {
          currentFilter = nextFilter
          onFilterChange(nextFilter)
        },
      }),
    )
    act(() => {
      mounted.findButtonByText('[Reset]').click()
    })
    expect(currentFilter.enabled).toBe(false)
    expect(currentFilter.modes).toEqual([
      expect.objectContaining({ modeId: 1, travel: true, discovery: true }),
      expect.objectContaining({ modeId: 2, travel: true, discovery: true }),
    ])
  })
})
