import { createElement } from 'react'
import { renderToString } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import { expedition } from '../../api/models'
import { CarrierRouteBottomPanel } from './CarrierRouteBottomPanel'
import { CarrierRouteModal } from './CarrierRouteModal'

describe('CarrierRoute UI Components', () => {
  it('CarrierRouteModal omits Manual Entry Mode and keeps QUERYING SPANSH... single-line', () => {
    const renderCarrierModal = (
      overrides: Partial<Parameters<typeof CarrierRouteModal>[0]['prompt']> = {},
    ) =>
      renderToString(
        createElement(CarrierRouteModal, {
          prompt: {
            targetRowId: 10,
            origin: 'Sol',
            destination: 'Colonia',
            capacityUsed: '5000',
            depotTritium: '1000',
            carrierType: 'player',
            loading: false,
            ...overrides,
          },
          onClose: () => {},
          onUpdate: () => {},
          onStart: () => {},
        }),
      )

    const idleHtml = renderCarrierModal()
    expect(idleHtml).not.toContain('Manual Entry Mode')
    expect(idleHtml).toContain('PLOT VIA SPANSH')
    expect(idleHtml).toContain('whitespace-nowrap')

    const loadingHtml = renderCarrierModal({ loading: true })
    expect(loadingHtml).toContain('QUERYING SPANSH...')
    expect(loadingHtml).toContain('whitespace-nowrap')
  })

  it('CarrierRouteBottomPanel renders Close button with tooltip, start/end indicator with ??? fallback, and Mark FC Stop Arrival button', () => {
    const renderCarrierPanel = (
      overrides: Partial<Parameters<typeof CarrierRouteBottomPanel>[0]> & {
        route: expedition.CarrierRouteResult
      },
    ) =>
      renderToString(
        createElement(CarrierRouteBottomPanel, {
          jumpIndex: 0,
          manualMode: true,
          hasNextFcStop: false,
          nextFcStopSystem: '',
          onCancelRoute: () => {},
          onToggleManualMode: () => {},
          onCompleteSpanshJump: () => {},
          manualDistanceInput: '',
          onChangeManualDistance: () => {},
          manualTritiumInput: '',
          onChangeManualTritium: () => {},
          onLogManualJump: () => {},
          manualLegJumps: 0,
          manualLegDistance: 0,
          manualLegTritium: 0,
          loggingJump: false,
          copiedNextJump: false,
          onCopySystem: () => {},
          onArrive: () => {},
          ...overrides,
        }),
      )

    const manualWithStop = renderCarrierPanel({
      route: expedition.CarrierRouteResult.createFrom({
        origin: 'Sol',
        destination: 'Colonia',
        carrierType: 'player',
        manualMode: true,
        jumps: [],
      }),
      hasNextFcStop: true,
      nextFcStopSystem: 'Colonia',
      manualDistanceInput: '412.5',
      manualTritiumInput: '59',
      manualLegJumps: 2,
      manualLegDistance: 950,
      manualLegTritium: 180,
    })

    expect(manualWithStop).not.toContain('Cancel Remaining Route')
    expect(manualWithStop).toContain('CLOSE')
    expect(manualWithStop).toContain(
      'Existing jumps remain logged in your expedition statistics.',
    )
    expect(manualWithStop).toContain('Sol')
    expect(manualWithStop).toContain('Colonia')
    expect(manualWithStop).toContain('value="412.5"')
    expect(manualWithStop).toContain('value="59"')
    expect(manualWithStop).toContain('MARK FC STOP ARRIVAL')
    expect(manualWithStop).toContain(
      'Mark Fleet Carrier arrival at Colonia and close the carrier router',
    )
    expect(manualWithStop).not.toContain('ARRIVE AT WAYPOINT')
    expect(manualWithStop).not.toMatch(/disabled=""[^>]*>MARK FC STOP ARRIVAL/)

    const manualWithoutStop = renderCarrierPanel({
      route: expedition.CarrierRouteResult.createFrom({
        origin: 'Sol',
        destination: '',
        carrierType: 'player',
        manualMode: true,
        jumps: [],
      }),
    })

    expect(manualWithoutStop).toContain('Sol')
    expect(manualWithoutStop).toContain('???')
    expect(manualWithoutStop).toContain('MARK FC STOP ARRIVAL')
    expect(manualWithoutStop).toContain(
      'No next Fleet Carrier stop designated in the expedition plan',
    )
    expect(manualWithoutStop).toMatch(/disabled=""[^>]*>MARK FC STOP ARRIVAL/)

    const plottedSpanshRoute = expedition.CarrierRouteResult.createFrom({
      origin: 'Sol',
      destination: 'Colonia',
      carrierType: 'player',
      manualMode: false,
      jumps: [
        {
          systemName: 'Rohini',
          distanceLy: 495.2,
          tritiumUsed: 68,
          fuelInTank: 932,
          hasIcyRing: true,
        },
        {
          systemName: 'Colonia',
          distanceLy: 310.4,
          tritiumUsed: 44,
          fuelInTank: 888,
          hasIcyRing: false,
        },
      ],
    })

    const spanshModeHtml = renderCarrierPanel({
      route: plottedSpanshRoute,
      jumpIndex: 0,
      manualMode: false,
      hasNextFcStop: true,
      nextFcStopSystem: 'Beagle Point',
    })

    expect(spanshModeHtml).toContain('Colonia')
    expect(spanshModeHtml).not.toContain('Beagle Point')
    expect(spanshModeHtml).toContain('805.6 Ly')
    expect(spanshModeHtml).toMatch(/~(?:<!-- -->)?112(?:<!-- -->)? t/)
    expect(spanshModeHtml).toContain('class="ed-btn-primary">COMPLETE JUMP #1</button>')

    const spanshFinalJumpHtml = renderCarrierPanel({
      route: plottedSpanshRoute,
      jumpIndex: 1,
      manualMode: false,
      hasNextFcStop: true,
      nextFcStopSystem: 'Colonia',
    })

    expect(spanshFinalJumpHtml).toContain(
      'title="Complete final carrier jump to Colonia and mark waypoint arrival"',
    )
    expect(spanshFinalJumpHtml).toContain('class="ed-btn-cyan">COMPLETE JUMP #2</button>')
    expect(spanshFinalJumpHtml).not.toContain('CARRIER ARRIVED')
  })
})

