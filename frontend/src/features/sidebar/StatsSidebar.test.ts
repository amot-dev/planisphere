import { createElement } from 'react'
import { renderToString } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import { app, stats } from '../../api/models'
import { NavigationTelemetrySection } from './NavigationTelemetrySection'
import { StatsSidebar } from './StatsSidebar'

describe('StatsSidebar', () => {
  it('renders LOG JUMP button only in active Expedition scope and disables it when carrierRouteOpen is true', () => {
    const emptyFilter = stats.Filter.createFrom({ enabled: false, modes: [] })
    const renderStatsSidebar = (overrides: Partial<Parameters<typeof StatsSidebar>[0]> = {}) =>
      renderToString(
        createElement(StatsSidebar, {
          sidebarStats: null,
          modesState: null,
          filter: emptyFilter,
          onFilterChange: () => {},
          onLogCarrierJump: () => {},
          ...overrides,
        }),
      )

    expect(renderStatsSidebar()).not.toContain('LOG JUMP')
    expect(renderStatsSidebar()).not.toContain('Flight Mode')
    expect(renderStatsSidebar()).toContain('Telemetry Filter')

    const planningHtml = renderStatsSidebar({
      sidebarStats: app.SidebarStats.createFrom({ expeditionId: 1, status: 'planning' }),
    })
    expect(planningHtml).not.toContain('LOG JUMP')

    const activeSidebarStats = app.SidebarStats.createFrom({ expeditionId: 1, status: 'active' })
    const html = renderStatsSidebar({ sidebarStats: activeSidebarStats })
    expect(html).toContain('LOG JUMP')
    expect(html).not.toContain('CARRIER LOG')
    expect(html).not.toMatch(/disabled=""[^>]*>LOG JUMP/)

    const openRouteHtml = renderStatsSidebar({
      sidebarStats: activeSidebarStats,
      carrierRouteOpen: true,
    })
    expect(openRouteHtml).toContain('Carrier routing panel is already open')
    expect(openRouteHtml).toMatch(/disabled=""[^>]*>LOG JUMP/)

    const completedHtml = renderStatsSidebar({
      sidebarStats: app.SidebarStats.createFrom({
        expeditionId: 1,
        status: 'completed',
        expedition: {
          carrierJumps: 2,
          carrierDistanceLy: 900,
          tritiumUsed: 150,
        },
        carrierCost: {
          carrierType: 'squadron',
          weeklyUpkeep: 14000000,
          tritiumPrice: 50000,
          tritiumCost: 7500000,
          jumpCost: 200000,
          upkeepCost: 14000000,
          totalCost: 21700000,
          totalEstimated: true,
        },
      }),
    })

    expect(completedHtml).not.toContain('LOG JUMP')
    expect(completedHtml).toContain('Flight Operations')
    expect(completedHtml).toContain('Expedition Carrier Operations')
    expect(completedHtml).toContain('Science Operations')
    expect(completedHtml).toContain('Carrier Cost')
    expect(completedHtml).toContain('~21,700,000 Cr')
    expect(completedHtml).toContain('Tritium Cost')
    expect(completedHtml).toContain('~7,500,000 Cr')
    expect(completedHtml).toContain('Jump Maintenance')
    expect(completedHtml).toContain('200,000 Cr')
    expect(completedHtml).not.toContain('~200,000 Cr')
    expect(completedHtml).toContain('Weekly Upkeep')
    expect(completedHtml).toContain('~14,000,000 Cr')
    expect(completedHtml).toContain(
      'Configure expedition Fleet Carrier vessel classification and weekly upkeep',
    )

    const allTimeSectionHtml = renderToString(
      createElement(NavigationTelemetrySection, {
        viewScope: 'allTime',
        sidebarStats: app.SidebarStats.createFrom({
          allJournals: {
            carrierDistanceLy: 12500,
            carrierJumps: 28,
            tritiumUsed: 1900,
          },
        }),
        displayedMetrics: stats.Metrics.createFrom({
          carrierDistanceLy: 12500,
          carrierJumps: 28,
          tritiumUsed: 1900,
        }),
        carrierRouteOpen: false,
      }),
    )

    expect(allTimeSectionHtml).toContain('Personal Carrier Operations')
    expect(allTimeSectionHtml).not.toContain('OWNED VESSEL')
    expect(allTimeSectionHtml).toContain('Personal Carrier Dist (Ly)')
    expect(allTimeSectionHtml).toContain('Personal Carrier Jumps')
    expect(allTimeSectionHtml).not.toContain('Personal Carrier Tritium')
    expect(allTimeSectionHtml).not.toContain('Carrier Cost')
  })

  it('renders NOT STARTED, AWAITING DEPARTURE, and active elapsed duration states in Expedition Duration', () => {
    const emptyMetrics = stats.Metrics.createFrom({})

    const planningHtml = renderToString(
      createElement(NavigationTelemetrySection, {
        viewScope: 'expedition',
        sidebarStats: app.SidebarStats.createFrom({
          expeditionId: 1,
          status: 'planning',
          started: false,
          departed: false,
        }),
        displayedMetrics: emptyMetrics,
        carrierRouteOpen: false,
      }),
    )
    expect(planningHtml).toContain('NOT STARTED')
    expect(planningHtml).not.toContain('AWAITING DEPARTURE')
    expect(planningHtml).not.toContain('START:')

    const stagedHtml = renderToString(
      createElement(NavigationTelemetrySection, {
        viewScope: 'expedition',
        sidebarStats: app.SidebarStats.createFrom({
          expeditionId: 1,
          status: 'active',
          started: true,
          departed: false,
          endIsNow: false,
        }),
        displayedMetrics: emptyMetrics,
        carrierRouteOpen: false,
      }),
    )
    expect(stagedHtml).toContain('AWAITING DEPARTURE')
    expect(stagedHtml).not.toContain('NOT STARTED')
    expect(stagedHtml).not.toContain('START:')
    expect(stagedHtml).not.toContain('CURRENT:')

    const underwayHtml = renderToString(
      createElement(NavigationTelemetrySection, {
        viewScope: 'expedition',
        sidebarStats: app.SidebarStats.createFrom({
          expeditionId: 1,
          status: 'active',
          started: true,
          departed: true,
          startGalactic: '3312-01-18 12:00 UTC',
          endIsNow: true,
          duration: '2h 15m',
        }),
        displayedMetrics: emptyMetrics,
        carrierRouteOpen: false,
      }),
    )
    expect(underwayHtml).toContain('2h 15m')
    expect(underwayHtml).toContain('START:')
    expect(underwayHtml).toContain('3312-01-18 12:00 UTC')
    expect(underwayHtml).toContain('CURRENT:')
    expect(underwayHtml).not.toContain('AWAITING DEPARTURE')
  })
})

