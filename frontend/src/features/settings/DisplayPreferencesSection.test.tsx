// @vitest-environment happy-dom
import { act, createElement } from 'react'
import { renderToString } from 'react-dom/server'
import { describe, expect, it, vi } from 'vitest'
import { app, stats } from '../../api/models'
import { makeSettings } from '../../test/fixtures'
import { mountTestRoot } from '../../test/render'
import { installWailsAppMock } from '../../test/wailsMock'
import { NavigationTelemetrySection } from '../sidebar/NavigationTelemetrySection'
import { DisplayPreferencesSection } from './DisplayPreferencesSection'

describe('DisplayPreferencesSection', () => {
  it('defaults to Calendar, persists In-Game selection via SetDurationDisplayMode, and reverts on error', async () => {
    const setDurationDisplayModeSpy = vi
      .fn<(mode: string) => Promise<void>>()
      .mockResolvedValueOnce(undefined)
      .mockRejectedValueOnce(new Error('save failed'))
    installWailsAppMock({
      SetDurationDisplayMode: setDurationDisplayModeSpy,
    })

    const onSetError = vi.fn()
    const mounted = mountTestRoot(
      createElement(DisplayPreferencesSection, {
        settings: makeSettings({ durationDisplayMode: 'calendar' }),
        onSetError,
      }),
    )

    expect(mounted.container.textContent).toContain('Display Preferences')
    expect(mounted.container.textContent).toContain(
      'Choose whether durations default to calendar time or in-game time. Hover any duration to inspect the other.',
    )

    const calendarButton = mounted.findButtonByText('Calendar')
    const inGameButton = mounted.findButtonByText('In-Game')
    expect(calendarButton.getAttribute('aria-pressed')).toBe('true')
    expect(inGameButton.getAttribute('aria-pressed')).toBe('false')

    await act(async () => {
      inGameButton.click()
      await Promise.resolve()
    })
    expect(setDurationDisplayModeSpy).toHaveBeenCalledWith('in_game')
    expect(inGameButton.getAttribute('aria-pressed')).toBe('true')
    expect(onSetError).toHaveBeenLastCalledWith('')

    // Failed toggle back to Calendar reverts selection to the last committed mode and reports the error.
    await act(async () => {
      calendarButton.click()
      await Promise.resolve()
    })
    expect(setDurationDisplayModeSpy).toHaveBeenCalledWith('calendar')
    expect(inGameButton.getAttribute('aria-pressed')).toBe('true')
    expect(calendarButton.getAttribute('aria-pressed')).toBe('false')
    expect(onSetError).toHaveBeenLastCalledWith(expect.stringContaining('save failed'))
  })

  it('swaps primary text and hover tooltip in NavigationTelemetrySection across calendar and in_game modes without underlines', () => {
    const emptyMetrics = stats.Metrics.createFrom({})
    const sidebarPayload = app.SidebarStats.createFrom({
      expeditionId: 1,
      status: 'active',
      started: true,
      departed: true,
      startGalactic: '3312-01-18 12:00 UTC',
      endIsNow: true,
      duration: '14d',
      inGameDuration: '32h 15m',
      careerStart: '2024-01-01T00:00:00Z',
      careerStartGalactic: '3310-01-01 00:00 UTC',
      careerDuration: '2y 10d',
      careerInGameDuration: '1,954h 58m',
    })

    const expCalendarHtml = renderToString(
      createElement(NavigationTelemetrySection, {
        viewScope: 'expedition',
        sidebarStats: sidebarPayload,
        displayedMetrics: emptyMetrics,
        carrierRouteOpen: false,
        durationDisplayMode: 'calendar',
      }),
    )
    expect(expCalendarHtml).toContain('title="In-game time: 32h 15m"')
    expect(expCalendarHtml).toContain('>14d</span>')
    expect(expCalendarHtml).not.toContain('underline')

    const expInGameHtml = renderToString(
      createElement(NavigationTelemetrySection, {
        viewScope: 'expedition',
        sidebarStats: sidebarPayload,
        displayedMetrics: emptyMetrics,
        carrierRouteOpen: false,
        durationDisplayMode: 'in_game',
      }),
    )
    expect(expInGameHtml).toContain('title="Calendar elapsed: 14d"')
    expect(expInGameHtml).toContain('>32h 15m</span>')

    const careerInGameHtml = renderToString(
      createElement(NavigationTelemetrySection, {
        viewScope: 'allTime',
        sidebarStats: sidebarPayload,
        displayedMetrics: emptyMetrics,
        carrierRouteOpen: false,
        durationDisplayMode: 'in_game',
      }),
    )
    expect(careerInGameHtml).toContain('title="Calendar elapsed: 2y 10d"')
    expect(careerInGameHtml).toContain('>1,954h 58m</span>')
  })
})
