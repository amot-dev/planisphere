import { useEffect, useMemo, useRef, useState } from 'react'
import {
  GetExpeditionExportData,
  PickExportSavePath,
  WriteExportOutput,
} from '../../api/backend'
import { exporter } from '../../api/models'
import { ModalShell } from '../../components/ui/ModalShell'
import { SquareCheckbox } from '../../components/ui/SquareCheckbox'
import { BlockConfigurator } from './BlockConfigurator'
import { ExportBlockRenderer } from './blocks/ExportBlockRenderer'
import { EXPORT_CARD_CSS, EXPORT_CARD_WIDTH } from './exportCardStyles'
import { ExportCartQueue } from './ExportCartQueue'
import { buildCanonicalJsonSnapshot } from './exportHighlights'
import {
  computeAvailableDetailsMetricIds,
  createQuickShareCart,
  expandConfigToCartItems,
  slugify,
} from './exportMetrics'
import {
  CartBlockItem,
  ConfiguratorBlockConfig,
  createDefaultBlockConfig,
  ExpeditionExportData,
  ExportBlockKind,
  ExportBundleFile,
  ExportOutputOptions,
} from './exportTypes'
import { generateSelfContainedHtmlReport } from './htmlGenerator'
import { renderCartToPngFiles } from './pngRenderer'

/** Props for the Export Studio modal dialog. */
export interface ExportExpeditionModalProps {
  expeditionId: number
  onClose: () => void
}

/**
 * ExportExpeditionModal hosts the split-screen Export Studio with sticky block
 * configuration, Quick Share preset, Live Preview, and PNG/HTML/JSON export dispatch.
 */
export function ExportExpeditionModal({ expeditionId, onClose }: ExportExpeditionModalProps) {
  const [exportData, setExportData] = useState<ExpeditionExportData | null>(null)
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState<string | null>(null)

  const [config, setConfig] = useState<ConfiguratorBlockConfig>(() =>
    createDefaultBlockConfig('summary'),
  )
  const [cartItems, setCartItems] = useState<CartBlockItem[]>([])
  const [inspectedCartItemId, setInspectedCartItemId] = useState<string | null>(null)
  const [previewSliceIndex, setPreviewSliceIndex] = useState(0)
  const [previewZoomMode, setPreviewZoomMode] = useState<'fit' | 'actual'>('fit')

  const [outputs, setOutputs] = useState<ExportOutputOptions>({
    includePng: true,
    includeHtml: false,
    includeJson: false,
  })
  const [exporting, setExporting] = useState(false)
  const [exportStatus, setExportStatus] = useState<string | null>(null)
  const [exportError, setExportError] = useState<string | null>(null)

  const addCounterRef = useRef(1)
  const previewContainerRef = useRef<HTMLDivElement | null>(null)
  const [fitScale, setFitScale] = useState(0.65)

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    setLoadError(null)
    GetExpeditionExportData(expeditionId)
      .then((payload) => {
        if (cancelled) return
        const typedPayload = payload as unknown as ExpeditionExportData
        setExportData(typedPayload)
        setCartItems(createQuickShareCart(typedPayload))
      })
      .catch((error: unknown) => {
        if (cancelled) return
        setLoadError(error instanceof Error ? error.message : String(error))
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [expeditionId])

  useEffect(() => {
    const element = previewContainerRef.current
    if (!element) return
    const updateScale = () => {
      const availableWidth = Math.max(320, element.clientWidth - 28)
      setFitScale(Math.min(1, availableWidth / EXPORT_CARD_WIDTH))
    }
    updateScale()
    const observer = new ResizeObserver(updateScale)
    observer.observe(element)
    return () => observer.disconnect()
  }, [exportData])

  const expandedPreviewItems = useMemo(() => {
    if (!exportData) return []
    return expandConfigToCartItems(exportData, config, 'preview')
  }, [config, exportData])

  useEffect(() => {
    setPreviewSliceIndex(0)
  }, [config])

  const activePreviewItem = useMemo(() => {
    if (inspectedCartItemId) {
      const found = cartItems.find((item) => item.id === inspectedCartItemId)
      if (found) return found
    }
    if (expandedPreviewItems.length === 0) return null
    const clampedIndex = Math.min(previewSliceIndex, expandedPreviewItems.length - 1)
    return expandedPreviewItems[clampedIndex]
  }, [cartItems, expandedPreviewItems, inspectedCartItemId, previewSliceIndex])

  const availableDetailsMetricIds = useMemo(() => {
    if (!exportData) return null
    return computeAvailableDetailsMetricIds(exportData, config, expandedPreviewItems)
  }, [config, expandedPreviewItems, exportData])

  const canAddToCart = useMemo(() => {
    if (!exportData || expandedPreviewItems.length === 0) return false
    if (config.kind === 'highlights') {
      return config.selectedCards.length > 0
    }
    if (config.kind === 'details' && config.section !== 'all' && availableDetailsMetricIds) {
      return config.selectedMetrics.some((id) => availableDetailsMetricIds.has(id))
    }
    return true
  }, [availableDetailsMetricIds, config, expandedPreviewItems.length, exportData])

  const outputPlan = useMemo(() => {
    const pngCount = outputs.includePng ? cartItems.length : 0
    const htmlCount = outputs.includeHtml && cartItems.length > 0 ? 1 : 0
    const jsonCount = outputs.includeJson ? 1 : 0
    const totalFiles = pngCount + htmlCount + jsonCount
    let fileExt = 'zip'
    if (totalFiles === 1) {
      fileExt = pngCount === 1 ? 'png' : htmlCount === 1 ? 'html' : 'json'
    }
    const summaryParts: string[] = []
    if (pngCount > 0) summaryParts.push(`${pngCount} PNG${pngCount === 1 ? '' : 's'}`)
    if (htmlCount > 0) summaryParts.push('1 HTML')
    if (jsonCount > 0) summaryParts.push('1 JSON')
    return { pngCount, htmlCount, jsonCount, totalFiles, fileExt, summaryParts }
  }, [cartItems.length, outputs])

  function handleSelectBlockKind(nextKind: ExportBlockKind) {
    setInspectedCartItemId(null)
    setConfig(createDefaultBlockConfig(nextKind))
  }

  function handleConfigChange(nextConfig: ConfiguratorBlockConfig) {
    setInspectedCartItemId(null)
    setConfig(nextConfig)
  }

  function handleAddToCart() {
    if (!exportData || !canAddToCart) return
    const batchId = `item-${addCounterRef.current++}`
    const addedItems = expandConfigToCartItems(exportData, config, batchId)
    setCartItems((previous) => [...previous, ...addedItems])
    setExportStatus(null)
    setExportError(null)
  }

  function handleMoveCartItem(index: number, direction: -1 | 1) {
    setCartItems((previous) => {
      const targetIndex = index + direction
      if (targetIndex < 0 || targetIndex >= previous.length) return previous
      const next = [...previous]
      const [moved] = next.splice(index, 1)
      next.splice(targetIndex, 0, moved)
      return next
    })
  }

  function handleRemoveCartItem(index: number) {
    setCartItems((previous) => {
      const removed = previous[index]
      if (removed && removed.id === inspectedCartItemId) {
        setInspectedCartItemId(null)
      }
      return previous.filter((_, itemIndex) => itemIndex !== index)
    })
  }

  async function handleRunExport() {
    if (!exportData || outputPlan.totalFiles === 0) return
    setExporting(true)
    setExportError(null)
    setExportStatus(null)
    try {
      const expeditionSlug = slugify(exportData.plan.expedition.name)
      const defaultFilename =
        outputPlan.totalFiles === 1
          ? `${expeditionSlug}-${outputPlan.fileExt === 'png' ? cartItems[0].filenameSlug : outputPlan.fileExt === 'html' ? 'report' : 'snapshot'}.${outputPlan.fileExt}`
          : `${expeditionSlug}-export.zip`

      const targetPath = await PickExportSavePath(defaultFilename, outputPlan.fileExt)
      if (!targetPath) {
        setExporting(false)
        return
      }

      const bundleFiles: ExportBundleFile[] = []
      if (outputPlan.pngCount > 0) {
        setExportStatus(`Rendering PNG 0 / ${cartItems.length}...`)
        const pngFiles = await renderCartToPngFiles(
          exportData,
          cartItems,
          (done, total) => setExportStatus(`Rendering PNG ${done} / ${total}...`),
        )
        bundleFiles.push(...pngFiles)
      }
      if (outputPlan.htmlCount > 0) {
        setExportStatus('Building offline HTML report...')
        bundleFiles.push({
          name: `${expeditionSlug}-report.html`,
          contentText: generateSelfContainedHtmlReport(exportData, cartItems),
        })
      }
      if (outputPlan.jsonCount > 0) {
        setExportStatus('Serializing canonical JSON snapshot...')
        bundleFiles.push({
          name: `${expeditionSlug}-snapshot.json`,
          contentText: buildCanonicalJsonSnapshot(exportData),
        })
      }

      setExportStatus('Writing export output...')
      const wireFiles = bundleFiles.map((file) => exporter.BundleFile.createFrom(file))
      const savedPath = await WriteExportOutput(targetPath, wireFiles)
      setExportStatus(`Saved to ${savedPath}`)
    } catch (error: unknown) {
      setExportError(error instanceof Error ? error.message : String(error))
      setExportStatus(null)
    } finally {
      setExporting(false)
    }
  }

  const footer = (
    <div className="px-5 py-3 border-t border-ed-border bg-ed-header flex flex-wrap items-center justify-between gap-3 text-xs">
      <div className="flex flex-wrap items-center gap-4">
        <button
          type="button"
          disabled={exporting}
          onClick={() => setOutputs((prev) => ({ ...prev, includePng: !prev.includePng }))}
          className={`inline-flex items-center gap-2 text-ed-text-main ${
            exporting ? 'opacity-40 cursor-not-allowed' : 'cursor-pointer'
          }`}
        >
          <SquareCheckbox checked={outputs.includePng} disabled={exporting} size="xs" />
          <span className="font-bold uppercase tracking-wider text-[11px]">PNG Images</span>
        </button>
        <button
          type="button"
          disabled={exporting}
          onClick={() => setOutputs((prev) => ({ ...prev, includeHtml: !prev.includeHtml }))}
          className={`inline-flex items-center gap-2 text-ed-text-main ${
            exporting ? 'opacity-40 cursor-not-allowed' : 'cursor-pointer'
          }`}
        >
          <SquareCheckbox checked={outputs.includeHtml} disabled={exporting} size="xs" />
          <span className="font-bold uppercase tracking-wider text-[11px]">HTML Report</span>
        </button>
        <button
          type="button"
          disabled={exporting}
          onClick={() => setOutputs((prev) => ({ ...prev, includeJson: !prev.includeJson }))}
          className={`inline-flex items-center gap-2 text-ed-text-main ${
            exporting ? 'opacity-40 cursor-not-allowed' : 'cursor-pointer'
          }`}
        >
          <SquareCheckbox checked={outputs.includeJson} disabled={exporting} size="xs" />
          <span className="font-bold uppercase tracking-wider text-[11px]">JSON Data</span>
        </button>
      </div>

      <div className="flex items-center gap-3">
        <span className="font-mono-tabular text-[11px] text-ed-text-muted">
          {outputPlan.totalFiles === 0
            ? 'Select at least one output'
            : `${outputPlan.summaryParts.join(' + ')} -> .${outputPlan.fileExt}`}
        </span>
        <button
          type="button"
          disabled={exporting}
          onClick={onClose}
          className={`px-3 py-1.5 text-[11px] font-bold uppercase tracking-wider border border-ed-border bg-ed-control text-ed-text-muted ${
            exporting ? 'opacity-40 cursor-not-allowed' : 'hover:text-ed-text-main cursor-pointer'
          }`}
        >
          Close
        </button>
        <button
          type="button"
          disabled={exporting || loading || !exportData || outputPlan.totalFiles === 0}
          onClick={() => void handleRunExport()}
          className={`ed-btn-primary ${
            exporting || loading || !exportData || outputPlan.totalFiles === 0
              ? 'opacity-40 cursor-not-allowed'
              : ''
          }`}
        >
          {exporting ? 'EXPORTING...' : 'EXPORT'}
        </button>
      </div>
    </div>
  )

  return (
    <ModalShell
      title="Export Studio"
      subtitle={
        exportData
          ? `${exportData.plan.expedition.name} · ${exportData.plan.visitedWaypointCount} / ${exportData.plan.waypointCount} Waypoints Visited`
          : 'Loading expedition telemetry...'
      }
      badge={exportData ? exportData.plan.expedition.status.toUpperCase() : undefined}
      accent="cyan"
      maxWidth="max-w-6xl"
      onClose={exporting ? undefined : onClose}
      closeDisabled={exporting}
      bodyClassName="flex-1 min-h-0 overflow-hidden flex flex-col"
      footer={footer}
    >
      <style>{EXPORT_CARD_CSS}</style>

      {loading ? (
        <div className="p-10 text-center font-mono-tabular text-xs text-ed-amber uppercase tracking-widest">
          [ ASSEMBLING EXPEDITION TELEMETRY SNAPSHOT... ]
        </div>
      ) : loadError || !exportData ? (
        <div className="p-8 text-center text-xs text-ed-danger-text font-mono-tabular">
          {loadError || 'Unable to load expedition export snapshot.'}
        </div>
      ) : (
        <div className="flex-1 min-h-0 flex flex-col lg:flex-row overflow-hidden">
          <div className="w-full lg:w-[360px] shrink-0 border-b lg:border-b-0 lg:border-r border-ed-border bg-ed-rail p-4 overflow-y-auto space-y-4">
            <BlockConfigurator
              exportData={exportData}
              config={config}
              availableDetailsMetricIds={availableDetailsMetricIds}
              addCount={expandedPreviewItems.length}
              canAddToCart={canAddToCart}
              disabled={exporting}
              onSelectBlockKind={handleSelectBlockKind}
              onChangeConfig={handleConfigChange}
              onAddToCart={handleAddToCart}
            />
            <ExportCartQueue
              items={cartItems}
              inspectedCartItemId={inspectedCartItemId}
              disabled={exporting}
              onApplyQuickShare={() => {
                setInspectedCartItemId(null)
                setCartItems(createQuickShareCart(exportData))
              }}
              onClearCart={() => {
                setInspectedCartItemId(null)
                setCartItems([])
              }}
              onInspectItem={(itemId) => setInspectedCartItemId(itemId)}
              onMoveItem={handleMoveCartItem}
              onRemoveItem={handleRemoveCartItem}
            />
          </div>

          <div className="flex-1 min-w-0 min-h-0 flex flex-col bg-ed-void overflow-hidden">
            <div className="px-4 py-2 border-b border-ed-border bg-ed-card-alt flex items-center justify-between gap-2 text-[10px]">
              <div className="flex items-center gap-2 font-mono-tabular uppercase tracking-wider text-ed-text-muted">
                <span>
                  {inspectedCartItemId
                    ? 'Inspecting Cart Block'
                    : 'Live Preview (Active Config)'}
                </span>
                {inspectedCartItemId && (
                  <button
                    type="button"
                    disabled={exporting}
                    onClick={() => setInspectedCartItemId(null)}
                    className={`text-ed-cyan ${
                      exporting ? 'opacity-40 cursor-not-allowed' : 'hover:underline cursor-pointer'
                    }`}
                  >
                    [Back to Config]
                  </button>
                )}
                {!inspectedCartItemId && expandedPreviewItems.length > 1 && (
                  <span className="inline-flex items-center gap-1.5 text-ed-amber">
                    <button
                      type="button"
                      disabled={exporting || previewSliceIndex <= 0}
                      onClick={() => setPreviewSliceIndex((idx) => Math.max(0, idx - 1))}
                      className={`px-1 border border-ed-border ${
                        exporting || previewSliceIndex <= 0
                          ? 'opacity-30 cursor-not-allowed'
                          : 'cursor-pointer'
                      }`}
                    >
                      Prev
                    </button>
                    <span>
                      {previewSliceIndex + 1} / {expandedPreviewItems.length}
                    </span>
                    <button
                      type="button"
                      disabled={exporting || previewSliceIndex >= expandedPreviewItems.length - 1}
                      onClick={() =>
                        setPreviewSliceIndex((idx) =>
                          Math.min(expandedPreviewItems.length - 1, idx + 1),
                        )
                      }
                      className={`px-1 border border-ed-border ${
                        exporting || previewSliceIndex >= expandedPreviewItems.length - 1
                          ? 'opacity-30 cursor-not-allowed'
                          : 'cursor-pointer'
                      }`}
                    >
                      Next
                    </button>
                  </span>
                )}
              </div>

              <div className="flex items-center gap-1 font-mono-tabular">
                {(['fit', 'actual'] as const).map((zoomMode) => (
                  <button
                    key={zoomMode}
                    type="button"
                    onClick={() => setPreviewZoomMode(zoomMode)}
                    className={`px-2 py-0.5 border uppercase cursor-pointer ${
                      previewZoomMode === zoomMode
                        ? 'border-ed-amber text-ed-amber bg-ed-amber-tint'
                        : 'border-ed-border text-ed-text-muted'
                    }`}
                  >
                    {zoomMode === 'fit' ? 'Fit' : '100%'}
                  </button>
                ))}
              </div>
            </div>

            {(exportStatus || exportError) && (
              <div
                className={`px-4 py-2 border-b text-[11px] font-mono-tabular ${
                  exportError
                    ? 'bg-ed-danger-tint border-ed-danger-border text-ed-danger-text'
                    : 'bg-ed-cyan-tint border-ed-cyan-border text-ed-cyan'
                }`}
              >
                {exportError ?? exportStatus}
              </div>
            )}

            <div
              ref={previewContainerRef}
              className="flex-1 min-h-0 overflow-auto p-4 flex justify-center items-start"
            >
              {activePreviewItem ? (
                <div
                  style={
                    previewZoomMode === 'fit'
                      ? {
                          transform: `scale(${fitScale})`,
                          transformOrigin: 'top center',
                          width: `${EXPORT_CARD_WIDTH}px`,
                        }
                      : { width: `${EXPORT_CARD_WIDTH}px` }
                  }
                >
                  <ExportBlockRenderer
                    exportData={exportData}
                    config={activePreviewItem.config}
                  />
                </div>
              ) : (
                <div className="p-8 text-xs font-mono-tabular text-ed-text-muted">
                  Configure a block on the left to preview it here.
                </div>
              )}
            </div>
          </div>
        </div>
      )}
    </ModalShell>
  )
}

