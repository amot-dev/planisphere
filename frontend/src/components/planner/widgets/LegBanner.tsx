import { memo } from 'react'
import { expedition, modes } from '../../../../wailsjs/go/models'
import { LegModeDropdown } from './LegModeDropdown'

/**
 * LegBannerProps defines the ship transition log and active flight mode state
 * for an expedition leg summary strip.
 */
export interface LegBannerProps {
  transitions: expedition.Transition[]
  modeId: number
  modeColor?: string
  modesList: modes.Mode[]
  onChangeMode: (nextModeId: number) => void
}

/**
 * LegBanner renders the vessels flown and interactive flight mode selector
 * for the current in-progress expedition leg.
 */
export const LegBanner = memo(function LegBanner({
  transitions,
  modeId,
  modeColor,
  modesList,
  onChangeMode,
}: LegBannerProps) {
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

