// @vitest-environment happy-dom
import { act, createElement } from 'react'
import { describe, expect, it, vi } from 'vitest'
import { app, expedition } from '../../api/models'
import { makeSettings } from '../../test/fixtures'
import { mountTestRoot, setFormControlValue } from '../../test/render'
import { APP_PRODUCT_NAME, APP_TAGLINE } from '../../version'
import { CockpitTopBar } from './CockpitTopBar'

describe('CockpitTopBar', () => {
  const sampleExpeditions = [
    expedition.Expedition.createFrom({ id: 1, name: 'Perseus Reach', status: 'active' }),
    expedition.Expedition.createFrom({ id: 2, name: 'Colonia Bridge', status: 'planning' }),
  ]

  it('renders active expedition switcher, telemetry badges, sync pause toggle, and modal triggers', () => {
    const onToggleSyncPause = vi.fn()
    const onSelectExpedition = vi.fn()
    const onOpenExpeditionsModal = vi.fn()
    const onOpenSettingsModal = vi.fn()
    const onOpenSearchModal = vi.fn()

    const mounted = mountTestRoot(
      createElement(CockpitTopBar, {
        sidebarCollapsed: false,
        expeditions: sampleExpeditions,
        activeExpeditionId: 1,
        activeStatusMessage: 'COMPUTING TELEMETRY',
        syncPauseState: 'active',
        hasPendingSyncUpdates: false,
        ingest: app.IngestStatus.createFrom({
          currentSystem: 'Shinrarta Dezhra',
          currentShip: 'Mandalay',
        }),
        settings: makeSettings(),
        onToggleSyncPause,
        onSelectExpedition,
        onOpenExpeditionsModal,
        onOpenSettingsModal,
        onOpenSearchModal,
      }),
    )

    const headerText = mounted.container.textContent ?? ''
    expect(headerText).toContain(APP_PRODUCT_NAME)
    expect(headerText).toContain(APP_TAGLINE)
    expect(headerText).toContain('Perseus Reach [ACTIVE]')
    expect(headerText).toContain('Colonia Bridge [PLANNING]')
    expect(headerText).toContain('Shinrarta Dezhra')
    expect(headerText).toContain('Mandalay')
    expect(mounted.container.querySelector('.ed-scan-bar')).not.toBeNull()

    const expeditionSelect = mounted.container.querySelector(
      'select[title="Select active expedition"]',
    ) as HTMLSelectElement
    act(() => {
      setFormControlValue(expeditionSelect, '2')
      mounted.findButtonByText('EXPEDITIONS').click()
      mounted.findButtonByText('CONFIG').click()
      mounted.findButtonByText('BUSY').click()
    })

    const searchButton = mounted.container.querySelector(
      'button[aria-label="Search waypoints and visit history"]',
    ) as HTMLButtonElement
    act(() => searchButton.click())

    expect(onSelectExpedition).toHaveBeenCalledWith(2)
    expect(onOpenExpeditionsModal).toHaveBeenCalledTimes(1)
    expect(onOpenSettingsModal).toHaveBeenCalledTimes(1)
    expect(onToggleSyncPause).toHaveBeenCalledTimes(1)
    expect(onOpenSearchModal).toHaveBeenCalledTimes(1)

    // Rerendering collapsed without search handler or active status hides optional elements.
    mounted.rerender(
      createElement(CockpitTopBar, {
        sidebarCollapsed: true,
        expeditions: sampleExpeditions,
        activeExpeditionId: 2,
        activeStatusMessage: null,
        syncPauseState: 'paused',
        hasPendingSyncUpdates: true,
        ingest: null,
        settings: null,
        onToggleSyncPause,
        onSelectExpedition,
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
