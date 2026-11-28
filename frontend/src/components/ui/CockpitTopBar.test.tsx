// @vitest-environment happy-dom
import { act, createElement } from 'react'
import { describe, expect, it, vi } from 'vitest'
import { app } from '../../api/models'
import { makeSettings } from '../../test/fixtures'
import { mountTestRoot } from '../../test/render'
import { APP_PRODUCT_NAME, APP_TAGLINE } from '../../version'
import { CockpitTopBar } from './CockpitTopBar'

describe('CockpitTopBar', () => {
  it('renders telemetry badges, sync pause toggle, and modal triggers without an expedition dropdown', () => {
    const onToggleSyncPause = vi.fn()
    const onOpenExpeditionsModal = vi.fn()
    const onOpenSettingsModal = vi.fn()
    const onOpenSearchModal = vi.fn()

    const mounted = mountTestRoot(
      createElement(CockpitTopBar, {
        sidebarCollapsed: false,
        activeStatusMessage: 'COMPUTING TELEMETRY',
        syncPauseState: 'active',
        hasPendingSyncUpdates: false,
        ingest: app.IngestStatus.createFrom({
          currentSystem: 'Shinrarta Dezhra',
          currentShip: 'Mandalay',
        }),
        settings: makeSettings(),
        onToggleSyncPause,
        onOpenExpeditionsModal,
        onOpenSettingsModal,
        onOpenSearchModal,
      }),
    )

    const headerText = mounted.container.textContent ?? ''
    expect(headerText).toContain(APP_PRODUCT_NAME)
    expect(headerText).toContain(APP_TAGLINE)
    expect(headerText).toContain('Shinrarta Dezhra')
    expect(headerText).toContain('Mandalay')
    expect(mounted.container.querySelector('select')).toBeNull()
    expect(mounted.container.querySelector('.ed-scan-bar')).not.toBeNull()

    act(() => {
      mounted.findButtonByText('EXPEDITIONS').click()
      mounted.findButtonByText('CONFIG').click()
      mounted.findButtonByText('BUSY').click()
    })

    const searchButton = mounted.container.querySelector(
      'button[aria-label="Search waypoints and visit history"]',
    ) as HTMLButtonElement
    act(() => searchButton.click())

    expect(onOpenExpeditionsModal).toHaveBeenCalledTimes(1)
    expect(onOpenSettingsModal).toHaveBeenCalledTimes(1)
    expect(onToggleSyncPause).toHaveBeenCalledTimes(1)
    expect(onOpenSearchModal).toHaveBeenCalledTimes(1)

    // Rerendering collapsed without search handler or active status hides optional elements.
    mounted.rerender(
      createElement(CockpitTopBar, {
        sidebarCollapsed: true,
        activeStatusMessage: null,
        syncPauseState: 'paused',
        hasPendingSyncUpdates: true,
        ingest: null,
        settings: null,
        onToggleSyncPause,
        onOpenExpeditionsModal,
        onOpenSettingsModal,
      }),
    )

    expect(
      mounted.container.querySelector('button[aria-label="Search waypoints and visit history"]'),
    ).toBeNull()
    expect(mounted.container.querySelector('.ed-scan-bar')).toBeNull()
    expect(mounted.container.textContent).not.toContain('Shinrarta Dezhra')
    expect(mounted.findButtonByText('PAUSED').getAttribute('aria-pressed')).toBe('true')
  })
})
