import { memo, useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { GetRegionOptions, OpenURL, RetryResolve } from '../../wailsjs/go/app/App'
import { app, expedition, modes } from '../../wailsjs/go/models'
import {
  edsmSystemUrl,
  formatCredits,
  formatInt,
  formatLy,
  inaraSystemUrl,
  nowGalacticTime,
} from '../utils/galacticTime'

/**
 * Isolated, self-updating Galactic Standard Time badge.
 * Updating here prevents 1Hz root re-renders of App, ExpeditionPlanner, and StatsSidebar.
 */
export function GalacticClockBadge() {
  const [time, setTime] = useState(() => nowGalacticTime())
  useEffect(() => {
    const clockTimer = setInterval(() => setTime(nowGalacticTime()), 1000)
    return () => clearInterval(clockTimer)
  }, [])
  return (
    <div
      className="px-2.5 py-1 bg-[#081318] border border-[#16485c] font-mono-tabular text-[11px] font-semibold text-[#00D2FF]"
      title="Galactic Standard Time (+1286 years)"
    >
      {time}
    </div>
  )
}

export interface RegionDropdownProps {
  expeditionId: number
  rowId: number
  currentRegionId: number
  regionName: string
  locked?: boolean
  onSelect: (regionId: number) => void
}

export const RegionHeaderDropdown = memo(function RegionHeaderDropdown({
  expeditionId,
  rowId,
  currentRegionId,
  regionName,
  locked = false,
  onSelect,
}: RegionDropdownProps) {
  const [regionOptions, setRegionOptions] = useState<app.RegionOptions | null>(null)
  const [open, setOpen] = useState(false)

  useEffect(() => {
    if (!open) return
    GetRegionOptions(expeditionId, rowId, true)
      .then(setRegionOptions)
      .catch(console.error)
  }, [open, expeditionId, rowId])

  if (locked) {
    return (
      <span className="text-sm font-extrabold tracking-[0.12em] uppercase text-[#FF7100]">
        {regionName || 'SELECT REGION'}
      </span>
    )
  }

  const adjacentList = regionOptions?.options?.filter((option) => option.adjacent) ?? []
  const anchorOption = regionOptions?.options?.find((option) => option.anchor)
  const otherList = regionOptions?.options?.filter((option) => !option.adjacent && !option.anchor) ?? []
  const suggestedOption = regionOptions?.options?.find((option) => option.id === regionOptions.suggestedRegionId)

  return (
    <div className="relative inline-block">
      <button
        type="button"
        onClick={() => setOpen((previousOpen) => !previousOpen)}
        className="inline-flex items-center gap-1.5 text-sm font-extrabold tracking-[0.12em] uppercase text-[#FF7100] hover:text-[#ff9436] cursor-pointer"
        title="Click to switch Galactic Region"
      >
        <span>{regionName || 'SELECT REGION'}</span>
        <svg
          className="w-3.5 h-3.5 opacity-60 group-hover:opacity-100"
          viewBox="0 0 20 20"
          fill="currentColor"
        >
          <path
            fillRule="evenodd"
            d="M5.23 7.21a.75.75 0 011.06.02L10 11.168l3.71-3.938a.75.75 0 111.08 1.04l-4.25 4.5a.75.75 0 01-1.08 0l-4.25-4.5a.75.75 0 01.02-1.06z"
            clipRule="evenodd"
          />
        </svg>
      </button>

      {open && (
        <>
          <div className="fixed inset-0 z-30" onClick={() => setOpen(false)} />
          <div className="absolute left-0 mt-1.5 w-80 max-h-96 overflow-y-auto z-40 border border-[#FF7100] bg-[#1c140d] shadow-2xl shadow-black text-xs">
            {regionOptions && regionOptions.suggestedRegionId > 0 && suggestedOption && (
              <div className="p-2 border-b border-[#3d2816] bg-[#241a10]">
                <button
                  type="button"
                  onClick={() => {
                    onSelect(regionOptions.suggestedRegionId)
                    setOpen(false)
                  }}
                  className="w-full text-left px-2 py-1 bg-[#08151c] border border-[#00D2FF] text-[#00D2FF] hover:bg-[#00D2FF] hover:text-[#050506] font-bold uppercase tracking-wider transition cursor-pointer"
                >
                  Suggested from next waypoint: {suggestedOption.name}
                </button>
              </div>
            )}

            {adjacentList.length > 0 && (
              <div>
                <div className="px-3 py-1.5 text-[10px] font-extrabold uppercase tracking-wider text-[#FF7100] bg-[#2b190c] border-b border-[#3d2816]">
                  Adjacent to {regionOptions?.anchorName} ({adjacentList.length})
                </div>
                {adjacentList.map((region) => (
                  <button
                    key={region.id}
                    type="button"
                    onClick={() => {
                      onSelect(region.id)
                      setOpen(false)
                    }}
                    className={`w-full flex items-center justify-between px-3 py-1.5 text-left hover:bg-[#FF7100] hover:text-[#050506] transition cursor-pointer ${
                      region.id === currentRegionId
                        ? 'bg-[#2b190c] text-[#FF7100] font-bold'
                        : 'text-[#F4ECE4]'
                    }`}
                  >
                    <span>{region.name}</span>
                    <span className="text-[9px] font-mono-tabular px-1.5 py-0.5 bg-[#16100b] text-[#FF7100] border border-[#472e19]">
                      ADJACENT
                    </span>
                  </button>
                ))}
              </div>
            )}

            {anchorOption && (
              <div>
                <div className="px-3 py-1 text-[10px] font-extrabold uppercase tracking-wider text-[#00D2FF] bg-[#08151c] border-y border-[#16485c]">
                  Anchor Region
                </div>
                <button
                  type="button"
                  onClick={() => {
                    onSelect(anchorOption.id)
                    setOpen(false)
                  }}
                  className={`w-full flex items-center justify-between px-3 py-1.5 text-left hover:bg-[#00D2FF] hover:text-[#050506] transition cursor-pointer ${
                    anchorOption.id === currentRegionId
                      ? 'bg-[#08151c] text-[#00D2FF] font-bold'
                      : 'text-[#F4ECE4]'
                  }`}
                >
                  <span>{anchorOption.name}</span>
                  <span className="text-[9px] font-mono-tabular px-1.5 py-0.5 bg-[#08151c] text-[#00D2FF] border border-[#16485c]">
                    ANCHOR
                  </span>
                </button>
              </div>
            )}

            <div>
              <div className="px-3 py-1.5 text-[10px] font-extrabold uppercase tracking-wider text-[#a6907c] bg-[#241a10] border-y border-[#3d2816]">
                All Other Galactic Regions ({otherList.length})
              </div>
              {otherList.map((region) => (
                <button
                  key={region.id}
                  type="button"
                  onClick={() => {
                    onSelect(region.id)
                    setOpen(false)
                  }}
                  className={`w-full flex items-center justify-between px-3 py-1.5 text-left hover:bg-[#FF7100] hover:text-[#050506] transition cursor-pointer ${
                    region.id === currentRegionId
                      ? 'bg-[#211307] text-[#FF7100] font-bold'
                      : 'text-[#d6c7b8]'
                  }`}
                >
                  <span>{region.name}</span>
                </button>
              ))}
            </div>
          </div>
        </>
      )}
    </div>
  )
})

export function LegModeDropdown({
  modeId,
  modeColor,
  modesList,
  onChangeMode,
}: {
  modeId: number
  modeColor?: string
  modesList: modes.Mode[]
  onChangeMode: (nextModeId: number) => void
}) {
  const [open, setOpen] = useState(false)
  const currentMode = modesList.find((mode) => mode.id === modeId) || modesList[0]

  return (
    <div className="relative inline-block">
      <button
        type="button"
        onClick={() => setOpen((previousOpen) => !previousOpen)}
        title="Active mode for this leg (click to change)"
        className="inline-flex items-center gap-2 px-2.5 py-0.5 text-[10px] font-mono-tabular uppercase tracking-wider border border-[#472e19] border-l-2 border-l-[#FF7100] bg-[#16100b] text-[#FF7100] hover:bg-[#24180e] transition cursor-pointer"
      >
        <span className="font-bold">[MODE]:</span>
        <span
          className="w-1.5 h-2.5 shrink-0"
          style={{ backgroundColor: modeColor || currentMode?.color || '#FF7100' }}
        />
        <span className="font-bold text-[#F4ECE4]">{currentMode?.name || 'TRAVEL'}</span>
        <svg
          className={`w-3 h-3 text-[#a6907c] transition-transform ${open ? 'rotate-180' : ''}`}
          viewBox="0 0 20 20"
          fill="currentColor"
        >
          <path
            fillRule="evenodd"
            d="M5.23 7.21a.75.75 0 011.06.02L10 11.168l3.71-3.938a.75.75 0 111.08 1.04l-4.25 4.5a.75.75 0 01-1.08 0l-4.25-4.5a.75.75 0 01.02-1.06z"
            clipRule="evenodd"
          />
        </svg>
      </button>

      {open && (
        <>
          <div className="fixed inset-0 z-30" onClick={() => setOpen(false)} />
          <div className="absolute left-0 mt-1 w-48 border border-[#FF7100] bg-[#1c140d] shadow-2xl shadow-black z-40 py-1 text-xs">
            <div className="px-2.5 py-1 text-[9px] font-extrabold uppercase tracking-wider text-[#a6907c] bg-[#241a10] border-b border-[#3d2816]">
              Select Leg Mode
            </div>
            {modesList.map((mode) => {
              const isSelected = mode.id === modeId
              return (
                <button
                  key={mode.id}
                  type="button"
                  onClick={() => {
                    onChangeMode(mode.id)
                    setOpen(false)
                  }}
                  className={`w-full flex items-center justify-between px-3 py-1.5 text-left transition cursor-pointer ${
                    isSelected
                      ? 'bg-[#2b190c] text-[#FF7100] font-bold'
                      : 'text-[#F4ECE4] hover:bg-[#FF7100] hover:text-[#050506]'
                  }`}
                >
                  <div className="flex items-center gap-2">
                    <span
                      className="w-2 h-2 shrink-0"
                      style={{ backgroundColor: mode.color || '#FF7100' }}
                    />
                    <span className="font-mono-tabular uppercase tracking-wider text-[11px]">
                      {mode.name}
                    </span>
                  </div>
                  {isSelected && <span className="text-[10px] font-bold">✓</span>}
                </button>
              )
            })}
          </div>
        </>
      )}
    </div>
  )
}

export const LegBanner = memo(function LegBanner({
  transitions,
  modeId,
  modeColor,
  modesList,
  onChangeMode,
}: {
  transitions: expedition.Transition[]
  modeId: number
  modeColor?: string
  modesList: modes.Mode[]
  onChangeMode: (nextModeId: number) => void
}) {
  const shipsUsed = Array.from(
    new Set(
      (transitions ?? [])
        .filter((transition) => transition.kind === 'ship' && transition.to)
        .map((transition) => transition.to),
    ),
  )

  return (
    <div className="flex flex-wrap items-center gap-2.5 px-4 py-1.5 bg-[#0e0a07] border-y border-[#2e1f12]">
      <div className="inline-flex items-center gap-2 px-2.5 py-0.5 text-[10px] font-mono-tabular uppercase tracking-wider border border-[#472e19] border-l-2 border-l-[#FF7100] bg-[#16100b] text-[#FF7100]">
        <span className="font-bold">
          {shipsUsed.length > 1 ? '[VESSELS FLOWN]:' : '[VESSEL FLOWN]:'}
        </span>
        <span className={`font-bold ${shipsUsed.length > 0 ? 'text-[#F4ECE4]' : 'text-[#a6907c]'}`}>
          {shipsUsed.length > 0 ? shipsUsed.join(' // ') : 'None'}
        </span>
      </div>

      <LegModeDropdown
        modeId={modeId}
        modeColor={modeColor}
        modesList={modesList}
        onChangeMode={onChangeMode}
      />
    </div>
  )
})

export const HoverOpenInLinks = memo(function HoverOpenInLinks({
  rowView,
}: {
  rowView: expedition.RowView
}) {
  const systemName = rowView.row.systemName?.trim()
  if (!systemName) return null
  const inara = inaraSystemUrl(systemName)
  const edsm = edsmSystemUrl(systemName)

  return (
    <div className="inline-flex items-center gap-1 opacity-85 group-hover:opacity-100 shrink-0 whitespace-nowrap">
      <button
        type="button"
        onClick={(event) => {
          event.stopPropagation()
          OpenURL(inara).catch(console.error)
        }}
        title={`Open ${systemName} on Inara (${inara})`}
        className="h-5 px-1.5 inline-flex items-center justify-center whitespace-nowrap text-[9px] font-mono-tabular font-bold uppercase tracking-wider bg-[#16100b] hover:bg-[#FF7100] text-[#FF7100] hover:text-[#050506] border border-[#472e19] cursor-pointer"
      >
        INARA ↗
      </button>
      <button
        type="button"
        onClick={(event) => {
          event.stopPropagation()
          OpenURL(edsm).catch(console.error)
        }}
        title={`Open ${systemName} on EDSM (${edsm})`}
        className="h-5 px-1.5 inline-flex items-center justify-center whitespace-nowrap text-[9px] font-mono-tabular font-bold uppercase tracking-wider bg-[#08151c] hover:bg-[#00D2FF] text-[#00D2FF] hover:text-[#050506] border border-[#16485c] cursor-pointer"
      >
        EDSM ↗
      </button>
      {rowView.gec?.url && (
        <button
          type="button"
          onClick={(event) => {
            event.stopPropagation()
            OpenURL(rowView.gec!.url).catch(console.error)
          }}
          title={`EDAstro GEC POI: ${rowView.gec.name} (${rowView.gec.type})`}
          className="h-5 px-1.5 inline-flex items-center justify-center whitespace-nowrap text-[9px] font-mono-tabular font-bold uppercase tracking-wider bg-[#211307] hover:bg-[#FF7100] text-[#FF7100] hover:text-[#050506] border border-[#FF7100] cursor-pointer"
        >
          GEC ↗
        </button>
      )}
    </div>
  )
})

export function WaypointDetailsDrawer({
  details,
  loading,
  rowView,
}: {
  details: expedition.WaypointDetails | null
  loading: boolean
  rowView: expedition.RowView
}) {
  if (loading || !details) {
    return (
      <div className="px-6 py-3 bg-[#0b0907] border-t border-[#3d2816] text-xs text-[#FF7100] font-mono-tabular uppercase tracking-wider">
        [ COMPUTING WAYPOINT TELEMETRY... ]
      </div>
    )
  }

  if (details.kind === 'future') {
    return (
      <div className="px-6 py-3.5 bg-[#0b0907] border-t border-[#3d2816] border-l-3 border-l-[#00D2FF] flex flex-wrap items-center justify-between gap-4 text-xs">
        <div className="flex flex-wrap items-center gap-6">
          <div>
            <span className="text-[10px] font-bold uppercase tracking-wider text-[#a6907c] block">
              Est. Straight-Line Distance (from Preceding Waypoint
              {details.prevSystem ? `: ${details.prevSystem}` : ''})
            </span>
            <span className="font-mono-tabular text-sm font-bold text-[#00D2FF]">
              {details.estDistFromPrevLy !== undefined
                ? formatLy(details.estDistFromPrevLy)
                : 'COORDINATES PENDING / UNKNOWN'}
            </span>
          </div>
          <div>
            <span className="text-[10px] font-bold uppercase tracking-wider text-[#a6907c] block">
              Est. Straight-Line Distance (from Current Location
              {details.currentSystem ? `: ${details.currentSystem}` : ''})
            </span>
            <span className="font-mono-tabular text-sm font-bold text-[#FF7100]">
              {details.estDistFromCurrentLy !== undefined
                ? formatLy(details.estDistFromCurrentLy)
                : '—'}
            </span>
          </div>
          {rowView.row.hasCoords && (
            <div>
              <span className="text-[10px] font-bold uppercase tracking-wider text-[#a6907c] block">
                Galactic Coordinates (X, Y, Z)
              </span>
              <span className="font-mono-tabular text-xs text-[#F4ECE4]">
                ({rowView.row.x.toFixed(2)}, {rowView.row.y.toFixed(2)}, {rowView.row.z.toFixed(2)})
                {rowView.detectedRegion && ` · ${rowView.detectedRegion}`}
              </span>
            </div>
          )}
        </div>

        {!rowView.row.hasCoords && rowView.row.systemName && (
          <button
            type="button"
            onClick={() => RetryResolve(rowView.row.id).catch(console.error)}
            className="ed-btn"
          >
            Retry Coordinate Lookup
          </button>
        )}
      </div>
    )
  }

  const metrics = details.metrics
  const isOrigin = details.kind === 'origin'

  return (
    <div className="px-6 py-4 bg-[#0b0907] border-t border-[#3d2816] border-l-3 border-l-[#FF7100] text-xs space-y-3">
      <div className="flex flex-wrap items-center justify-between gap-2 border-b border-[#2e1f12] pb-2">
        <div className="flex items-center gap-2">
          <span className="px-2 py-0.5 text-[10px] font-mono-tabular font-extrabold uppercase tracking-[0.14em] bg-[#211307] text-[#FF7100] border border-[#FF7100]">
            {isOrigin
              ? 'WAYPOINT #1 // HISTORICAL BASELINE (ARCHIVE START → EXPEDITION START)'
              : `LEG #${details.number - 1} → #${details.number} // DELTA TELEMETRY`}
          </span>
          {details.prevSystem && !isOrigin && (
            <span className="font-mono-tabular text-[#a6907c]">
              {details.prevSystem} →{' '}
              <span className="text-[#F4ECE4] font-bold">{rowView.row.systemName}</span>
            </span>
          )}
        </div>
        <div className="font-mono-tabular text-[11px] text-[#a6907c]">
          {details.fromGalactic || 'Earliest Archive'} →{' '}
          <span className="text-[#00D2FF] font-bold">{details.toGalactic}</span>
          {details.elapsed && <span className="ml-2 text-[#F4ECE4]">[{details.elapsed}]</span>}
        </div>
      </div>

      <div className="grid grid-cols-2 sm:grid-cols-4 gap-2.5">
        <MetricMiniCard
          label="Distance Travelled"
          value={formatLy(metrics.distanceLy)}
          sub={`Ship: ${formatLy(
            metrics.shipDistanceLy || Math.max(0, metrics.distanceLy - (metrics.carrierPassengerLy || 0)),
            1,
          )} · Carrier: ${formatLy(metrics.carrierPassengerLy || 0, 1)}`}
          accent="amber"
        />
        <MetricMiniCard label="Hyperspace Jumps" value={formatInt(metrics.jumps)} />
        <MetricMiniCard
          label="Boosts (Jet/Pr/St/Ba)"
          value={`${metrics.jetConeBoosts} / ${metrics.premiumBoosts} / ${metrics.standardBoosts} / ${metrics.basicBoosts}`}
        />
        <MetricMiniCard
          label="Scanned / Mapped"
          value={`${formatInt(metrics.bodiesScanned)} / ${formatInt(metrics.bodiesMapped)}`}
          sub={`First: ${metrics.firstDiscoveries} disc · ${metrics.firstMapped} map`}
        />
      </div>

      <div className="grid grid-cols-1 md:grid-cols-2 gap-3 pt-1">
        <div className="p-2.5 border border-[#3d2816] border-l-2 border-l-[#00D2FF] bg-[#120d09]">
          <div className="flex items-center justify-between gap-2">
            <span className="text-[11px] uppercase tracking-wider text-[#a6907c] font-bold truncate">
              Cartographics Value
            </span>
            <span className="font-mono-tabular text-sm font-extrabold text-[#00D2FF] whitespace-nowrap shrink-0">
              {formatCredits(metrics.scanValueTotal)}
            </span>
          </div>
          <div className="mt-1.5 grid grid-cols-2 gap-2 pt-1.5 border-t border-[#2e1f12] text-[11px]">
            <div className="flex justify-between gap-2 text-[#a6907c]">
              <span className="truncate">Base Scans &amp; Mapping:</span>
              <span className="font-mono-tabular text-[#F4ECE4] whitespace-nowrap shrink-0">
                {formatCredits(metrics.scanValueBase)}
              </span>
            </div>
            <div className="flex justify-between gap-2 text-[#a6907c]">
              <span className="truncate">First Disc. / Mapped:</span>
              <span className="font-mono-tabular text-[#00D2FF] font-bold whitespace-nowrap shrink-0">
                +{formatCredits(metrics.scanValueBonus)}
              </span>
            </div>
          </div>
        </div>

        <div className="p-2.5 border border-[#3d2816] border-l-2 border-l-[#00E5A3] bg-[#120d09]">
          <div className="flex items-center justify-between gap-2">
            <span className="text-[11px] uppercase tracking-wider text-[#a6907c] font-bold truncate">
              Exobiology Value ({formatInt(metrics.organicSamples)} samples)
            </span>
            <span className="font-mono-tabular text-sm font-extrabold text-[#00E5A3] whitespace-nowrap shrink-0">
              {formatCredits(metrics.organicValueTotal)}
            </span>
          </div>
          <div className="mt-1.5 grid grid-cols-2 gap-2 pt-1.5 border-t border-[#2e1f12] text-[11px]">
            <div className="flex justify-between gap-2 text-[#a6907c]">
              <span className="truncate">Base Organic Value:</span>
              <span className="font-mono-tabular text-[#F4ECE4] whitespace-nowrap shrink-0">
                {formatCredits(metrics.organicValueBase)}
              </span>
            </div>
            <div className="flex justify-between gap-2 text-[#a6907c]">
              <span className="truncate">First Logged Bonus:</span>
              <span className="font-mono-tabular text-[#00E5A3] font-bold whitespace-nowrap shrink-0">
                +{formatCredits(metrics.organicValueBonus)}
              </span>
            </div>
          </div>
        </div>
      </div>
    </div>
  )
}

function MetricMiniCard({
  label,
  value,
  sub,
  accent,
}: {
  label: string
  value: string
  sub?: string
  accent?: 'amber' | 'cyan'
}) {
  return (
    <div className="p-2 border border-[#3d2816] bg-[#120d09]">
      <div className="text-[10px] font-bold uppercase tracking-wider text-[#a6907c] truncate">
        {label}
      </div>
      <div
        className={`font-mono-tabular text-xs font-extrabold mt-0.5 ${
          accent === 'amber'
            ? 'text-[#FF7100]'
            : accent === 'cyan'
              ? 'text-[#00D2FF]'
              : 'text-[#F4ECE4]'
        }`}
      >
        {value}
      </div>
      {sub && <div className="text-[10px] font-mono-tabular text-[#a6907c] mt-0.5">{sub}</div>}
    </div>
  )
}

/**
 * StatusBadge renders the high-level expedition lifecycle state (Planning / Active / Completed).
 */
export function StatusBadge({ status, armedAt }: { status: string; armedAt: string }) {
  if (status === 'active') {
    return (
      <span className="inline-flex items-center gap-2 px-3 py-1 text-[11px] font-extrabold uppercase tracking-[0.14em] bg-[#211307] text-[#FF7100] border border-[#FF7100] border-l-3">
        <span className="w-2 h-2 bg-[#FF7100] animate-pulse" />
        {armedAt ? 'ARMED // AWAITING WAYPOINT #1' : 'ACTIVE EXPEDITION'}
      </span>
    )
  }
  if (status === 'completed') {
    return (
      <span className="inline-flex items-center gap-1.5 px-3 py-1 text-[11px] font-extrabold uppercase tracking-[0.14em] bg-[#08151c] text-[#00D2FF] border border-[#00D2FF] border-l-3">
        ■ COMPLETED
      </span>
    )
  }
  return (
    <span className="inline-flex items-center gap-1.5 px-3 py-1 text-[11px] font-extrabold uppercase tracking-[0.14em] bg-[#16100b] text-[#F4ECE4] border border-[#472e19] border-l-3 border-l-[#00D2FF]">
      PLANNING PHASE
    </span>
  )
}

/**
 * CoordStatusIndicator shows the resolution state and coordinate origin for a waypoint:
 * - Green (#00E599): Coordinate known from player's local journal history (previously visited).
 * - Cyan (#00D2FF): Coordinate known from EDSM community star map.
 * - Amber (#FF7100): System coordinate unknown / not found.
 */
export const CoordStatusIndicator = memo(function CoordStatusIndicator({
  rowView,
}: {
  rowView: expedition.RowView
}) {
  const row = rowView.row
  if (!row.systemName) return null
  if (row.hasCoords) {
    const isJournal = row.coordsSource?.toLowerCase() === 'journal'
    return (
      <span
        title={`Coordinates (${row.x.toFixed(1)}, ${row.y.toFixed(1)}, ${row.z.toFixed(1)}) via ${row.coordsSource.toUpperCase()}${
          isJournal ? ' (Previously Visited in Flight Logs)' : ' (EDSM Star Map)'
        }${rowView.detectedRegion ? ` — ${rowView.detectedRegion}` : ''}`}
        className={`w-1.5 h-3 shrink-0 ${isJournal ? 'bg-[#00E599]' : 'bg-[#00D2FF]'}`}
      />
    )
  }
  if (row.coordsSource === 'notfound' || row.coordsSource === 'error') {
    return (
      <span
        title="Coordinates not yet found in local journals or EDSM"
        className="w-1.5 h-3 bg-[#FF7100] shrink-0"
      />
    )
  }
  return (
    <span
      title="Resolving coordinates..."
      className="w-1.5 h-3 bg-[#a6907c] animate-pulse shrink-0"
    />
  )
})

export interface RowManipulationButtonsProps {
  index: number
  minIndex: number
  total: number
  rowId?: number
  insertMenuOpen: boolean
  onToggleInsertMenu: () => void
  onInsert: (type: 'waypoint' | 'region') => void
  onMove: (toIndex: number) => void
  onDelete: () => void
}

/**
 * RowManipulationButtons provides row management controls (up, down, insert, delete)
 * for unvisited or planning-phase rows.
 */
export const RowManipulationButtons = memo(function RowManipulationButtons({
  index,
  minIndex,
  total,
  insertMenuOpen,
  onToggleInsertMenu,
  onInsert,
  onMove,
  onDelete,
}: RowManipulationButtonsProps) {
  return (
    <div className="relative inline-flex items-center gap-0.5 opacity-80 group-hover:opacity-100 shrink-0 whitespace-nowrap">
      <button
        type="button"
        disabled={index <= minIndex}
        onClick={() => onMove(index - 1)}
        title="Move row up"
        className="h-5 px-1.5 inline-flex items-center justify-center bg-[#140e09] hover:bg-[#FF7100] text-[#a6907c] hover:text-[#050506] border border-[#2e1f12] disabled:opacity-25 cursor-pointer font-mono-tabular text-xs"
      >
        ↑
      </button>
      <button
        type="button"
        disabled={index >= total - 1}
        onClick={() => onMove(index + 1)}
        title="Move row down"
        className="h-5 px-1.5 inline-flex items-center justify-center bg-[#140e09] hover:bg-[#FF7100] text-[#a6907c] hover:text-[#050506] border border-[#2e1f12] disabled:opacity-25 cursor-pointer font-mono-tabular text-xs"
      >
        ↓
      </button>

      {/* Insert Menu Dropdown Anchor */}
      <div className="relative inline-block">
        <button
          type="button"
          onClick={onToggleInsertMenu}
          title="Insert row below"
          className="h-5 px-1.5 inline-flex items-center justify-center bg-[#140e09] hover:bg-[#00D2FF] text-[#00D2FF] hover:text-[#050506] border border-[#2e1f12] font-mono-tabular font-bold text-xs cursor-pointer"
        >
          +
        </button>

        {insertMenuOpen && (
          <>
            {/* Transparent backdrop to close dropdown when clicking outside */}
            <div
              className="fixed inset-0 z-20 cursor-default"
              onClick={onToggleInsertMenu}
            />
            {/* Vertically stacked menu aligning to the right of the '+' button (expanding safely leftward) */}
            <div className="absolute right-0 top-full mt-1 z-30 w-56 border border-[#FF7100] bg-[#0b0907] shadow-2xl shadow-black p-1 text-left text-xs flex flex-col whitespace-normal divide-y divide-[#24180e]">
              <button
                type="button"
                onClick={() => onInsert('waypoint')}
                className="w-full px-3 py-2 text-left hover:bg-[#FF7100] hover:text-[#050506] text-[#F4ECE4] font-bold uppercase tracking-wider cursor-pointer transition-colors"
              >
                + Insert Waypoint Below
              </button>
              <button
                type="button"
                onClick={() => onInsert('region')}
                className="w-full px-3 py-2 text-left hover:bg-[#FF7100] hover:text-[#050506] text-[#FF7100] font-bold uppercase tracking-wider cursor-pointer transition-colors"
              >
                + Insert Region Header Below
              </button>
            </div>
          </>
        )}
      </div>

      <button
        type="button"
        onClick={onDelete}
        title="Delete row"
        className="h-5 px-1.5 inline-flex items-center justify-center bg-[#140e09] hover:bg-[#ff3b2f] text-[#a6907c] hover:text-[#050506] border border-[#2e1f12] font-mono-tabular text-xs cursor-pointer"
      >
        ×
      </button>
    </div>
  )
})

export interface CarrierRouteBottomPanelProps {
  route: app.CarrierRouteResult
  jumpIndex: number
  manualMode: boolean
  onCancelRoute?: () => void
  onToggleManualMode: () => void
  onCompleteSpanshJump: () => void
  manualDistanceInput: string
  onChangeManualDistance: (value: string) => void
  manualTritiumInput: string
  onChangeManualTritium: (value: string) => void
  onLogManualJump: () => void
  manualLegJumps: number
  manualLegDistance: number
  manualLegTritium: number
  loggingJump: boolean
  copiedNextJump: boolean
  onCopySystem: (systemName: string) => void
  onArrive: () => void
}

/**
 * CarrierRouteBottomPanel renders the active fleet carrier navigation drawer,
 * supporting both Spansh 500 Ly calculated routes and manual jump logging.
 */
export function CarrierRouteBottomPanel({
  route,
  jumpIndex,
  manualMode,
  onCancelRoute,
  onToggleManualMode,
  onCompleteSpanshJump,
  manualDistanceInput,
  onChangeManualDistance,
  manualTritiumInput,
  onChangeManualTritium,
  onLogManualJump,
  manualLegJumps,
  manualLegDistance,
  manualLegTritium,
  loggingJump,
  copiedNextJump,
  onCopySystem,
  onArrive,
}: CarrierRouteBottomPanelProps) {
  const jumps = route.jumps ?? []
  const hasSpanshJumps = jumps.length > 0
  const currentSystem =
    jumpIndex === 0
      ? route.origin || 'Origin'
      : jumps[Math.min(jumpIndex - 1, jumps.length - 1)]?.systemName || route.destination
  const nextHop = jumpIndex < jumps.length ? jumps[jumpIndex] : null
  const remainingJumps = Math.max(0, jumps.length - jumpIndex)
  const remainingDistLy = jumps
    .slice(jumpIndex)
    .reduce((totalDistance, jump) => totalDistance + (jump.distanceLy || 0), 0)
  const remainingTrit = jumps
    .slice(jumpIndex)
    .reduce((totalTritium, jump) => totalTritium + (jump.tritiumUsed || 0), 0)

  return (
    <div className="border-t-2 border-[#00D2FF] bg-[#0b0907] px-5 py-3 shadow-2xl text-xs">
      <div className="flex flex-wrap items-center justify-between gap-2 pb-2.5 border-b border-[#2e1f12]">
        <div className="flex flex-wrap items-center gap-2.5">
          <span className="px-2 py-0.5 text-[10px] font-extrabold uppercase tracking-[0.14em] bg-[#08151c] text-[#00D2FF] border border-[#00D2FF]">
            {route.carrierType === 'squadron'
              ? 'SQUADRON CARRIER PLOTTER'
              : 'PERSONAL CARRIER PLOTTER'}
          </span>
          <span className="font-mono-tabular font-bold text-[#F4ECE4]">
            {route.origin || 'Origin'} <span className="text-[#a6907c]">→</span>{' '}
            <span className="text-[#FF7100]">{route.destination || 'Target Waypoint'}</span>
          </span>
          <span className="text-[11px] font-mono-tabular text-[#a6907c]">
            [{formatInt(route.capacityUsed)} t USED CAP]
          </span>
          {route.error && (
            <span className="text-[11px] text-[#FF7100] bg-[#211307] border border-[#FF7100] px-2 py-0.5 font-mono-tabular">
              {route.error}
            </span>
          )}
        </div>

        <div className="flex items-center gap-2">
          {!manualMode && hasSpanshJumps && (
            <button
              type="button"
              onClick={onToggleManualMode}
              className="ed-btn"
            >
              Switch to Manual Mode
            </button>
          )}
          {onCancelRoute && (
            <button
              type="button"
              onClick={onCancelRoute}
              title="Cancel the remaining hops of this route. Completed jumps will remain logged in your expedition statistics."
              className="ed-btn-danger"
            >
              Cancel Remaining Route
            </button>
          )}
        </div>
      </div>

      {!manualMode && hasSpanshJumps ? (
        <div className="pt-2.5 flex flex-wrap items-center justify-between gap-4">
          <div className="flex flex-wrap items-center gap-6">
            <div>
              <span className="text-[10px] font-bold uppercase tracking-wider text-[#a6907c] block">
                Current System
              </span>
              <span className="font-mono-tabular text-sm font-bold text-[#F4ECE4]">
                {currentSystem}
              </span>
            </div>

            <div>
              <span className="text-[10px] font-bold uppercase tracking-wider text-[#a6907c] block">
                Next Carrier Jump {nextHop ? `[#${jumpIndex + 1} OF ${jumps.length}]` : ''}
              </span>
              {nextHop ? (
                <div className="flex items-center gap-2">
                  <span className="font-mono-tabular text-sm font-extrabold text-[#FF7100]">
                    {nextHop.systemName}
                  </span>
                  <button
                    type="button"
                    onClick={() => onCopySystem(nextHop.systemName)}
                    title="Copy next jump system name to clipboard"
                    className="px-2 py-0.5 text-[10px] font-bold uppercase tracking-wider bg-[#140e09] hover:bg-[#FF7100] text-[#F4ECE4] hover:text-[#050506] border border-[#472e19] cursor-pointer"
                  >
                    {copiedNextJump ? 'COPIED ✓' : 'COPY'}
                  </button>
                  {nextHop.hasIcyRing && (
                    <span className="px-1.5 py-0.5 text-[9px] font-mono-tabular font-bold uppercase bg-[#08151c] text-[#00D2FF] border border-[#00D2FF]/50">
                      [ICY RING]
                    </span>
                  )}
                </div>
              ) : (
                <span className="font-mono-tabular text-sm font-bold text-[#00D2FF]">
                  ARRIVED AT {route.destination}
                </span>
              )}
            </div>

            {nextHop && (
              <div>
                <span className="text-[10px] font-bold uppercase tracking-wider text-[#a6907c] block">
                  Next Hop Telemetry
                </span>
                <span className="font-mono-tabular text-xs text-[#F4ECE4]">
                  {formatLy(nextHop.distanceLy, 2)} ·{' '}
                  <span className="text-[#00D2FF] font-bold">{nextHop.tritiumUsed} t TRITIUM</span>
                </span>
              </div>
            )}

            <div>
              <span className="text-[10px] font-bold uppercase tracking-wider text-[#a6907c] block">
                Remaining to Waypoint
              </span>
              <span className="font-mono-tabular text-xs text-[#F4ECE4]">
                {remainingJumps} {remainingJumps === 1 ? 'JUMP' : 'JUMPS'} ·{' '}
                <span className="text-[#FF7100] font-bold">{formatLy(remainingDistLy, 1)}</span> ·{' '}
                <span className="text-[#a6907c]">~{formatInt(remainingTrit)} t</span>
              </span>
            </div>
          </div>

          <div className="flex items-center gap-2">
            {nextHop ? (
              <button
                type="button"
                disabled={loggingJump}
                onClick={onCompleteSpanshJump}
                className="ed-btn-primary"
              >
                {loggingJump ? 'LOGGING JUMP...' : `COMPLETE JUMP #${jumpIndex + 1} ✓`}
              </button>
            ) : (
              <button
                type="button"
                onClick={onArrive}
                className="ed-btn-cyan"
              >
                ■ CARRIER ARRIVED — MARK ARRIVAL
              </button>
            )}
          </div>
        </div>
      ) : (
        <div className="pt-2.5 flex flex-wrap items-center justify-between gap-4">
          <div className="flex flex-wrap items-center gap-4">
            <div>
              <span className="text-[10px] font-bold uppercase tracking-wider text-[#a6907c] block">
                Manual Carrier Routing
              </span>
              <span className="font-mono-tabular text-xs text-[#F4ECE4]">
                Logged this leg:{' '}
                <span className="text-[#00D2FF] font-bold">{manualLegJumps} JUMPS</span> ·{' '}
                <span className="text-[#FF7100] font-bold">{formatLy(manualLegDistance, 1)}</span> ·{' '}
                <span>{formatInt(manualLegTritium)} t TRITIUM</span>
              </span>
            </div>

            <div className="flex items-center gap-2">
              <div>
                <label className="block text-[10px] font-bold uppercase tracking-wider text-[#a6907c] mb-0.5">
                  Jump Distance (Ly)
                </label>
                <input
                  type="number"
                  step="0.01"
                  min="0"
                  max="500"
                  value={manualDistanceInput}
                  onChange={(event) => onChangeManualDistance(event.target.value)}
                  placeholder="e.g. 498.4"
                  className="w-32 px-2.5 py-1 bg-[#050506] border border-[#3d2816] font-mono-tabular text-[#F4ECE4] focus:border-[#00D2FF] focus:outline-none"
                />
              </div>

              <div>
                <label className="block text-[10px] font-bold uppercase tracking-wider text-[#a6907c] mb-0.5">
                  Tritium Used (t)
                </label>
                <input
                  type="number"
                  step="1"
                  min="0"
                  value={manualTritiumInput}
                  onChange={(event) => onChangeManualTritium(event.target.value)}
                  placeholder="e.g. 68"
                  className="w-28 px-2.5 py-1 bg-[#050506] border border-[#3d2816] font-mono-tabular text-[#F4ECE4] focus:border-[#00D2FF] focus:outline-none"
                />
              </div>

              <div className="pt-4">
                <button
                  type="button"
                  disabled={loggingJump}
                  onClick={onLogManualJump}
                  className="ed-btn-primary"
                >
                  {loggingJump ? 'LOGGING...' : '+ LOG JUMP'}
                </button>
              </div>
            </div>
          </div>

          <button
            type="button"
            onClick={onArrive}
            className="ed-btn-cyan"
          >
            ■ ARRIVE AT WAYPOINT
          </button>
        </div>
      )}
    </div>
  )
}

const TEXTAREA_LINE_HEIGHT = 20 // 1.25rem = 20px (leading-5)
const TEXTAREA_VERTICAL_PAD = 10 // py-1 (8px) + border (2px)

export const AutoExpandingTextarea = memo(function AutoExpandingTextarea({
  value,
  placeholder,
  onChange,
  onBlur,
  className = '',
  maxLines = 3,
}: {
  value: string
  placeholder: string
  onChange: (value: string) => void
  onBlur: (value: string) => void
  className?: string
  maxLines?: number
}) {
  const textareaRef = useRef<HTMLTextAreaElement>(null)
  const [isFocused, setIsFocused] = useState(false)

  // Compute native HTML rows attribute so the browser layout handles height natively in C++ layout without JS reflows
  const rows = useMemo(() => {
    if (!value) return 1
    const explicit = value.split('\n').length
    const wrap = Math.ceil(value.length / 36)
    return Math.min(maxLines, Math.max(1, explicit, wrap))
  }, [value, maxLines])

  const adjustHeight = useCallback(() => {
    const textareaElement = textareaRef.current
    if (!textareaElement) return

    const minHeight = TEXTAREA_LINE_HEIGHT + TEXTAREA_VERTICAL_PAD
    const maxHeight = TEXTAREA_LINE_HEIGHT * maxLines + TEXTAREA_VERTICAL_PAD

    textareaElement.style.height = 'auto'
    const targetHeight = Math.min(Math.max(textareaElement.scrollHeight, minHeight), maxHeight)
    textareaElement.style.height = `${targetHeight}px`
    textareaElement.style.overflowY = textareaElement.scrollHeight > maxHeight ? 'auto' : 'hidden'
  }, [maxLines])

  // Only measure scrollHeight when actively focused and user is typing
  useLayoutEffect(() => {
    if (isFocused) {
      adjustHeight()
    }
  }, [value, isFocused, adjustHeight])

  return (
    <textarea
      ref={textareaRef}
      rows={rows}
      spellCheck={false}
      autoComplete="off"
      value={value}
      placeholder={placeholder}
      onFocus={() => {
        setIsFocused(true)
        adjustHeight()
      }}
      onChange={(event) => {
        onChange(event.target.value)
      }}
      onBlur={(event) => {
        setIsFocused(false)
        onBlur(event.target.value)
      }}
      onKeyDown={(event) => {
        if (event.key === 'Enter' && !event.shiftKey) {
          event.preventDefault()
          event.currentTarget.blur()
        }
      }}
      className={`w-full bg-transparent px-1.5 py-1 border border-transparent focus:border-[#FF7100] focus:bg-[#050506] focus:outline-none resize-none leading-5 text-xs block ${className}`}
    />
  )
})
