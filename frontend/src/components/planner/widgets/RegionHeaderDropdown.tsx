import { memo, useEffect, useState } from 'react'
import { GetRegionOptions } from '../../../../wailsjs/go/app/App'
import { app } from '../../../../wailsjs/go/models'

/**
 * RegionDropdownProps defines the current region selection and change callback
 * for a Galactic Region header row in the expedition planner.
 */
export interface RegionDropdownProps {
  expeditionId: number
  rowId: number
  currentRegionId: number
  regionName: string
  locked?: boolean
  onSelect: (regionId: number) => void
}

/**
 * RegionHeaderDropdown renders the interactive Galactic Region selector for a
 * region divider row, grouping suggested, adjacent, anchor, and remaining regions.
 */
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
        <RegionDropdownMenu
          regionOptions={regionOptions}
          currentRegionId={currentRegionId}
          onClose={() => setOpen(false)}
          onSelectRegion={(selectedRegionId) => {
            onSelect(selectedRegionId)
            setOpen(false)
          }}
        />
      )}
    </div>
  )
})

function RegionDropdownMenu({
  regionOptions,
  currentRegionId,
  onClose,
  onSelectRegion,
}: {
  regionOptions: app.RegionOptions | null
  currentRegionId: number
  onClose: () => void
  onSelectRegion: (regionId: number) => void
}) {
  const adjacentList = regionOptions?.options?.filter((option) => option.adjacent) ?? []
  const anchorOption = regionOptions?.options?.find((option) => option.anchor)
  const otherList =
    regionOptions?.options?.filter((option) => !option.adjacent && !option.anchor) ?? []
  const suggestedOption = regionOptions?.options?.find(
    (option) => option.id === regionOptions.suggestedRegionId,
  )

  return (
    <>
      <div className="fixed inset-0 z-30" onClick={onClose} />
      <div className="absolute left-0 mt-1.5 w-80 max-h-96 overflow-y-auto z-40 border border-[#FF7100] bg-[#1c140d] shadow-2xl shadow-black text-xs">
        {regionOptions && regionOptions.suggestedRegionId > 0 && suggestedOption && (
          <div className="p-2 border-b border-[#3d2816] bg-[#241a10]">
            <button
              type="button"
              onClick={() => onSelectRegion(regionOptions.suggestedRegionId)}
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
                onClick={() => onSelectRegion(region.id)}
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
              onClick={() => onSelectRegion(anchorOption.id)}
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
              onClick={() => onSelectRegion(region.id)}
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
  )
}

