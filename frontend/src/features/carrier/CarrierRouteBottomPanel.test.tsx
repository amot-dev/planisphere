import { type ComponentProps } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import { expedition } from '../../api/models'
import { CarrierRouteBottomPanel } from './CarrierRouteBottomPanel'
import { CarrierRouteModal } from './CarrierRouteModal'

describe('CarrierRouteModal and CarrierRouteBottomPanel', () => {
  it('CarrierRouteModal omits Manual Entry Mode and disables only PLOT button while querying Spansh', () => {
    const renderCarrierModal = (
      overrides: Partial<ComponentProps<typeof CarrierRouteModal>['prompt']> = {},
    ) =>
      renderToStaticMarkup(
        <CarrierRouteModal
          prompt={{
            targetRowId: 10,
            origin: 'Sol',
            destination: 'Colonia',
            capacityUsed: '5000',
            depotTritium: '1000',
            carrierType: 'player',
            loading: false,
            ...overrides,
          }}
          onClose={() => {}}
          onUpdate={() => {}}
          onStart={() => {}}
        />,
      )

    const idleHtml = renderCarrierModal()
    expect(idleHtml).not.toContain('Manual Entry Mode')
    expect(idleHtml).toContain('PLOT VIA SPANSH')

    const loadingHtml = renderCarrierModal({ loading: true })
    expect(loadingHtml).toContain('QUERYING SPANSH...')
    expect(loadingHtml.match(/<button[^>]*disabled=""/g)).toHaveLength(1)
    expect(loadingHtml).not.toMatch(/disabled=""[^>]*>CANCEL<\/button>/)
    expect(loadingHtml).toMatch(/disabled=""[^>]*>QUERYING SPANSH\.\.\.<\/button>/)
  })

  it('CarrierRouteBottomPanel renders Close button with tooltip, start/end indicator with ??? fallback, and Mark FC Stop Arrival button', () => {
    const renderCarrierPanel = (
      overrides: Partial<ComponentProps<typeof CarrierRouteBottomPanel>> & {
        route: expedition.CarrierRouteResult
      },
    ) =>
      renderToStaticMarkup(
        <CarrierRouteBottomPanel
          jumpIndex={0}
          manualMode={true}
          hasNextFcStop={false}
          nextFcStopSystem=""
          onCancelRoute={() => {}}
          onToggleManualMode={() => {}}
          onCompleteSpanshJump={() => {}}
          manualDistanceInput=""
          onChangeManualDistance={() => {}}
          manualTritiumInput=""
          onChangeManualTritium={() => {}}
          onLogManualJump={() => {}}
          manualLegJumps={0}
          manualLegDistance={0}
          manualLegTritium={0}
          loggingJump={false}
          copiedNextJump={false}
          onCopySystem={() => {}}
          onArrive={() => {}}
          {...overrides}
        />,
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
    expect(spanshModeHtml).toContain('~112 t')
    expect(spanshModeHtml).toContain('>COMPLETE JUMP #1</button>')

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
    expect(spanshFinalJumpHtml).toContain('>COMPLETE JUMP #2</button>')
    expect(spanshFinalJumpHtml).not.toContain('CARRIER ARRIVED')

    const spanshAllRecordedHtml = renderCarrierPanel({
      route: plottedSpanshRoute,
      jumpIndex: 2,
      depotTritium: 6,
      manualMode: false,
      hasNextFcStop: true,
      nextFcStopSystem: 'Colonia',
    })
    expect(spanshAllRecordedHtml).toContain('0 JUMPS')
    expect(spanshAllRecordedHtml).toContain('0.0 Ly')
    expect(spanshAllRecordedHtml).toContain('~0 t')
    expect(spanshAllRecordedHtml).toContain('>MARK FC STOP ARRIVAL</button>')
    expect(spanshAllRecordedHtml).not.toMatch(/disabled=""[^>]*>MARK FC STOP ARRIVAL/)

    const manualArrivalBusyHtml = renderCarrierPanel({
      route: expedition.CarrierRouteResult.createFrom({
        origin: 'Sol',
        destination: 'Colonia',
        carrierType: 'player',
        manualMode: true,
        jumps: [],
      }),
      hasNextFcStop: true,
      nextFcStopSystem: 'Colonia',
      loggingJump: true,
    })
    expect(manualArrivalBusyHtml).toMatch(/disabled=""[^>]*>MARK FC STOP ARRIVAL/)
  })
})
