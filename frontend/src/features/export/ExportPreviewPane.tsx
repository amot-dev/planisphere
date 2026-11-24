import { memo, RefObject, useEffect, useRef, useState, useSyncExternalStore } from 'react'
import { ExportBlockRenderer } from './blocks/ExportBlockRenderer'
import { EXPORT_CARD_WIDTH } from './exportCardStyles'
import { CartBlockItem, ExpeditionExportData } from './exportTypes'
import { ExportStatusStore } from './useExportStudio'

/** Props for the right-hand Export Studio live preview and zoom viewport. */
export interface ExportPreviewPaneProps {
  exportData: ExpeditionExportData
  expandedPreviewItems: ReadonlyArray<CartBlockItem>
  inspectedCartItem: CartBlockItem | null
  disabled?: boolean
  statusStore: ExportStatusStore
  onExitInspectMode: () => void
}

/**
 * ExportPreviewPane hosts the preview toolbar, leaf ExportStatusBanner, ResizeObserver
 * fit-to-viewport scaler, and memoized ExportBlockRenderer card preview.
 */
export const ExportPreviewPane = memo(function ExportPreviewPane(props: ExportPreviewPaneProps) {
  const { exportData, expandedPreviewItems, inspectedCartItem, disabled = false, statusStore, onExitInspectMode } = props
  const [previewSliceIndex, setPreviewSliceIndex] = useState(0)
  const [previewZoomMode, setPreviewZoomMode] = useState<'fit' | 'actual'>('fit')
  const previewContainerRef = useRef<HTMLDivElement | null>(null)
  const fitScale = usePreviewFitScale(previewContainerRef)

  useEffect(() => {
    setPreviewSliceIndex(0)
  }, [expandedPreviewItems])

  const clampedSliceIndex = expandedPreviewItems.length > 0 ? Math.min(previewSliceIndex, expandedPreviewItems.length - 1) : 0
  const activePreviewItem = inspectedCartItem ?? (expandedPreviewItems.length > 0 ? expandedPreviewItems[clampedSliceIndex] : null)
  const previewCardStyle = previewZoomMode === 'fit'
    ? { transform: `scale(${fitScale})`, transformOrigin: 'top center', width: `${EXPORT_CARD_WIDTH}px` }
    : { width: `${EXPORT_CARD_WIDTH}px` }

  return (
    <div className="flex-1 min-w-0 min-h-0 flex flex-col bg-ed-void overflow-hidden">
      <PreviewToolbar
        isInspectingCartItem={inspectedCartItem !== null}
        sliceIndex={clampedSliceIndex}
        sliceCount={expandedPreviewItems.length}
        zoomMode={previewZoomMode}
        disabled={disabled}
        onExitInspectMode={onExitInspectMode}
        onSelectSliceIndex={setPreviewSliceIndex}
        onSelectZoomMode={setPreviewZoomMode}
      />
      <ExportStatusBanner statusStore={statusStore} />
      <div ref={previewContainerRef} className="flex-1 min-h-0 overflow-auto p-4 flex justify-center items-start">
        {activePreviewItem ? (
          <div style={previewCardStyle}>
            <ExportBlockRenderer exportData={exportData} config={activePreviewItem.config} />
          </div>
        ) : (
          <div className="p-8 text-xs font-mono-tabular text-ed-text-muted">Configure a block on the left to preview it here.</div>
        )}
      </div>
    </div>
  )
})

function usePreviewFitScale(containerRef: RefObject<HTMLDivElement | null>): number {
  const [fitScale, setFitScale] = useState(0.65)
  useEffect(() => {
    const containerElement = containerRef.current
    if (!containerElement) return
    const updateScale = () => {
      const availableWidth = Math.max(320, containerElement.clientWidth - 28)
      setFitScale(Math.min(1, availableWidth / EXPORT_CARD_WIDTH))
    }
    updateScale()
    const observer = new ResizeObserver(updateScale)
    observer.observe(containerElement)
    return () => observer.disconnect()
  }, [containerRef])
  return fitScale
}

function PreviewToolbar(props: {
  isInspectingCartItem: boolean
  sliceIndex: number
  sliceCount: number
  zoomMode: 'fit' | 'actual'
  disabled: boolean
  onExitInspectMode: () => void
  onSelectSliceIndex: (nextIndex: number) => void
  onSelectZoomMode: (nextMode: 'fit' | 'actual') => void
}) {
  const { isInspectingCartItem, sliceIndex, sliceCount, zoomMode, disabled, onExitInspectMode, onSelectSliceIndex, onSelectZoomMode } = props
  return (
    <div className="px-4 py-2 border-b border-ed-border bg-ed-card-alt flex items-center justify-between gap-2 text-[10px]">
      <div className="flex items-center gap-2 font-mono-tabular uppercase tracking-wider text-ed-text-muted">
        <span>{isInspectingCartItem ? 'Inspecting Cart Block' : 'Live Preview (Active Config)'}</span>
        {isInspectingCartItem && (
          <button
            type="button"
            disabled={disabled}
            onClick={onExitInspectMode}
            className={`text-ed-cyan ${disabled ? 'opacity-40 cursor-not-allowed' : 'hover:underline cursor-pointer'}`}
          >
            [Back to Config]
          </button>
        )}
        {!isInspectingCartItem && sliceCount > 1 && (
          <PreviewSlicePager sliceIndex={sliceIndex} sliceCount={sliceCount} disabled={disabled} onSelectSliceIndex={onSelectSliceIndex} />
        )}
      </div>
      <div className="flex items-center gap-1 font-mono-tabular">
        {(['fit', 'actual'] as const).map((candidateMode) => (
          <button
            key={candidateMode}
            type="button"
            onClick={() => onSelectZoomMode(candidateMode)}
            className={`px-2 py-0.5 border uppercase cursor-pointer ${
              zoomMode === candidateMode ? 'border-ed-amber text-ed-amber bg-ed-amber-tint' : 'border-ed-border text-ed-text-muted'
            }`}
          >
            {candidateMode === 'fit' ? 'Fit' : '100%'}
          </button>
        ))}
      </div>
    </div>
  )
}

function PreviewSlicePager(props: {
  sliceIndex: number
  sliceCount: number
  disabled: boolean
  onSelectSliceIndex: (nextIndex: number) => void
}) {
  const { sliceIndex, sliceCount, disabled, onSelectSliceIndex } = props
  const prevDisabled = disabled || sliceIndex <= 0
  const nextDisabled = disabled || sliceIndex >= sliceCount - 1
  return (
    <span className="inline-flex items-center gap-1.5 text-ed-amber">
      <button type="button" disabled={prevDisabled} onClick={() => onSelectSliceIndex(Math.max(0, sliceIndex - 1))} className={`px-1 border border-ed-border ${prevDisabled ? 'opacity-30 cursor-not-allowed' : 'cursor-pointer'}`}>
        Prev
      </button>
      <span>{sliceIndex + 1} / {sliceCount}</span>
      <button type="button" disabled={nextDisabled} onClick={() => onSelectSliceIndex(Math.min(sliceCount - 1, sliceIndex + 1))} className={`px-1 border border-ed-border ${nextDisabled ? 'opacity-30 cursor-not-allowed' : 'cursor-pointer'}`}>
        Next
      </button>
    </span>
  )
}

function ExportStatusBanner({ statusStore }: { statusStore: ExportStatusStore }) {
  const { status, error } = useSyncExternalStore(statusStore.subscribe, statusStore.getSnapshot)
  if (!status && !error) return null
  return (
    <div
      className={`px-4 py-2 border-b text-[11px] font-mono-tabular ${
        error ? 'bg-ed-danger-tint border-ed-danger-border text-ed-danger-text' : 'bg-ed-cyan-tint border-ed-cyan-border text-ed-cyan'
      }`}
    >
      {error ?? status}
    </div>
  )
}
