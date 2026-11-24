import { Dispatch, SetStateAction, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { GetExpeditionExportData, PickExportSavePath, WriteExportOutput } from '../../api/backend'
import { exporter } from '../../api/models'
import {
  computeAvailableDetailsMetricIds, createQuickShareCart, expandConfigToCartItems, slugify,
} from './exportMetrics'
import {
  CartBlockItem, ConfiguratorBlockConfig, createDefaultBlockConfig, DetailsMetricId,
  ExpeditionExportData, ExportBlockConfig, ExportBlockKind, ExportBundleFile, ExportOutputOptions,
} from './exportTypes'
import { generateSelfContainedHtmlReport } from './htmlGenerator'
import { renderCartToPngFiles } from './pngRenderer'

/** Immutable snapshot of export progress or failure text read by ExportStatusBanner. */
export interface ExportStatusSnapshot {
  status: string | null
  error: string | null
}

/** Minimal external store allowing leaf status updates without parent re-renders. */
export interface ExportStatusStore {
  subscribe: (listener: () => void) => () => void
  getSnapshot: () => ExportStatusSnapshot
  setStatus: (status: string | null) => void
  setError: (error: string | null) => void
  clear: () => void
}

/** Precomputed output file counts and target archive extension for the footer bar. */
export interface ExportOutputPlan {
  pngCount: number
  htmlCount: number
  jsonCount: number
  totalFiles: number
  fileExt: string
  summaryParts: string[]
}

/** Complete Export Studio orchestration state and stable handlers returned by useExportStudio. */
export type ExportStudioState = ReturnType<typeof useExportStudio>

const EMPTY_EXPORT_STATUS: ExportStatusSnapshot = { status: null, error: null }

/** Creates an isolated status store instance for one mounted Export Studio session. */
export function createExportStatusStore(): ExportStatusStore {
  let currentSnapshot: ExportStatusSnapshot = EMPTY_EXPORT_STATUS
  const listeners = new Set<() => void>()
  const updateSnapshot = (nextSnapshot: ExportStatusSnapshot) => {
    if (currentSnapshot.status === nextSnapshot.status && currentSnapshot.error === nextSnapshot.error) return
    currentSnapshot = nextSnapshot
    for (const listener of listeners) listener()
  }
  return {
    subscribe: (listener) => {
      listeners.add(listener)
      return () => listeners.delete(listener)
    },
    getSnapshot: () => currentSnapshot,
    setStatus: (status) => updateSnapshot({ status, error: null }),
    setError: (error) => updateSnapshot({ status: null, error }),
    clear: () => updateSnapshot(EMPTY_EXPORT_STATUS),
  }
}

/**
 * useExportStudio manages expedition export snapshot loading, configurator and cart
 * state, memoized preview expansion, and isolated PNG, HTML, and JSON export dispatch.
 */
export function useExportStudio(expeditionId: number) {
  const statusStoreRef = useRef<ExportStatusStore | null>(null)
  if (!statusStoreRef.current) statusStoreRef.current = createExportStatusStore()
  const statusStore = statusStoreRef.current

  const [config, setConfig] = useState<ConfiguratorBlockConfig>(() => createDefaultBlockConfig('summary'))
  const [cartItems, setCartItems] = useState<CartBlockItem[]>([])
  const [inspectedCartItemId, setInspectedCartItemId] = useState<string | null>(null)
  const [outputs, setOutputs] = useState<ExportOutputOptions>({ includePng: true, includeHtml: false, includeJson: false })
  const [exporting, setExporting] = useState(false)
  const { exportData, loading, loadError } = useExpeditionExportSnapshot(expeditionId, setCartItems)

  const expandedPreviewItems = useMemo(
    () => (exportData ? expandConfigToCartItems(exportData, config, 'preview') : []),
    [config, exportData],
  )
  const inspectedCartItem = useMemo(
    () => (inspectedCartItemId ? cartItems.find((item) => item.id === inspectedCartItemId) ?? null : null),
    [cartItems, inspectedCartItemId],
  )
  const availableDetailsMetricIds = useMemo(() => {
    if (!exportData || config.kind !== 'details' || config.section === 'all') return null
    return computeAvailableDetailsMetricIds(exportData, config, expandedPreviewItems)
  }, [config, expandedPreviewItems, exportData])
  const canAddToCart = useMemo(
    () => computeCanAddToCart(exportData, config, expandedPreviewItems.length, availableDetailsMetricIds),
    [availableDetailsMetricIds, config, expandedPreviewItems.length, exportData],
  )
  const outputPlan = useMemo(() => computeExportOutputPlan(cartItems.length, outputs), [cartItems.length, outputs])
  const cartActions = useExportCartActions(
    exportData, canAddToCart, expandedPreviewItems, cartItems, inspectedCartItemId,
    statusStore, setConfig, setCartItems, setInspectedCartItemId, setOutputs,
  )
  const handleRunExport = useCallback(
    () => executeStudioExport(expeditionId, exportData, cartItems, outputPlan, statusStore, setExporting),
    [cartItems, expeditionId, exportData, outputPlan, statusStore],
  )

  return {
    exportData, loading, loadError, config, cartItems, inspectedCartItemId, inspectedCartItem,
    expandedPreviewItems, availableDetailsMetricIds, canAddToCart, outputs, outputPlan, exporting,
    statusStore, ...cartActions, handleRunExport,
  }
}

function useExpeditionExportSnapshot(expeditionId: number, setCartItems: Dispatch<SetStateAction<CartBlockItem[]>>) {
  const [exportData, setExportData] = useState<ExpeditionExportData | null>(null)
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    setLoadError(null)
    GetExpeditionExportData(expeditionId)
      .then((payload) => {
        if (cancelled) return
        setExportData(payload)
        setCartItems(createQuickShareCart(payload))
      })
      .catch((error: unknown) => { if (!cancelled) setLoadError(error instanceof Error ? error.message : String(error)) })
      .finally(() => { if (!cancelled) setLoading(false) })
    return () => { cancelled = true }
  }, [expeditionId, setCartItems])

  return { exportData, loading, loadError }
}

function useExportCartActions(
  exportData: ExpeditionExportData | null, canAddToCart: boolean, expandedPreviewItems: ReadonlyArray<CartBlockItem>,
  cartItems: CartBlockItem[], inspectedCartItemId: string | null, statusStore: ExportStatusStore,
  setConfig: Dispatch<SetStateAction<ConfiguratorBlockConfig>>, setCartItems: Dispatch<SetStateAction<CartBlockItem[]>>,
  setInspectedCartItemId: Dispatch<SetStateAction<string | null>>, setOutputs: Dispatch<SetStateAction<ExportOutputOptions>>,
) {
  const addCounterRef = useRef(1)
  const handleSelectBlockKind = useCallback((nextKind: ExportBlockKind) => { setInspectedCartItemId(null); setConfig(createDefaultBlockConfig(nextKind)) }, [setConfig, setInspectedCartItemId])
  const handleConfigChange = useCallback((nextConfig: ConfiguratorBlockConfig) => { setInspectedCartItemId(null); setConfig(nextConfig) }, [setConfig, setInspectedCartItemId])
  const handleAddToCart = useCallback(() => {
    if (!exportData || !canAddToCart || expandedPreviewItems.length === 0) return
    const batchId = `item-${addCounterRef.current++}`
    const addedItems = expandedPreviewItems.map((previewItem, index) => ({ ...previewItem, id: `${batchId}-${index}`, config: cloneCartBlockConfig(previewItem.config) }))
    setCartItems((previousItems) => [...previousItems, ...addedItems])
    statusStore.clear()
  }, [canAddToCart, expandedPreviewItems, exportData, setCartItems, statusStore])
  const handleApplyQuickShare = useCallback(() => {
    if (!exportData) return
    setInspectedCartItemId(null)
    setCartItems(createQuickShareCart(exportData))
  }, [exportData, setCartItems, setInspectedCartItemId])
  const handleClearCart = useCallback(() => { setInspectedCartItemId(null); setCartItems([]) }, [setCartItems, setInspectedCartItemId])
  const handleInspectCartItem = useCallback((itemId: string) => setInspectedCartItemId(itemId), [setInspectedCartItemId])
  const handleExitInspectMode = useCallback(() => setInspectedCartItemId(null), [setInspectedCartItemId])
  const handleMoveCartItem = useCallback((index: number, direction: -1 | 1) => setCartItems((previousItems) => moveCartItemAtIndex(previousItems, index, direction)), [setCartItems])
  const handleRemoveCartItem = useCallback((index: number) => {
    if (cartItems[index]?.id === inspectedCartItemId) setInspectedCartItemId(null)
    setCartItems((previousItems) => previousItems.filter((_, itemIndex) => itemIndex !== index))
  }, [cartItems, inspectedCartItemId, setCartItems, setInspectedCartItemId])
  const handleToggleOutput = useCallback((outputKey: keyof ExportOutputOptions) => setOutputs((previousOutputs) => ({ ...previousOutputs, [outputKey]: !previousOutputs[outputKey] })), [setOutputs])
  return {
    handleSelectBlockKind, handleConfigChange, handleAddToCart, handleApplyQuickShare, handleClearCart,
    handleInspectCartItem, handleExitInspectMode, handleMoveCartItem, handleRemoveCartItem, handleToggleOutput,
  }
}

function moveCartItemAtIndex(previousItems: CartBlockItem[], index: number, direction: -1 | 1): CartBlockItem[] {
  const targetIndex = index + direction
  if (targetIndex < 0 || targetIndex >= previousItems.length) return previousItems
  const nextItems = [...previousItems]
  const [movedItem] = nextItems.splice(index, 1)
  nextItems.splice(targetIndex, 0, movedItem)
  return nextItems
}

function computeCanAddToCart(
  exportData: ExpeditionExportData | null,
  config: ConfiguratorBlockConfig,
  expandedCount: number,
  availableDetailsMetricIds: ReadonlySet<DetailsMetricId> | null,
): boolean {
  if (!exportData || expandedCount === 0) return false
  if (config.kind === 'highlights') return config.selectedCards.length > 0
  if (config.kind === 'details' && config.section !== 'all' && availableDetailsMetricIds) {
    return config.selectedMetrics.some((metricId) => availableDetailsMetricIds.has(metricId))
  }
  return true
}

function computeExportOutputPlan(cartItemCount: number, outputs: ExportOutputOptions): ExportOutputPlan {
  const pngCount = outputs.includePng ? cartItemCount : 0
  const htmlCount = outputs.includeHtml && cartItemCount > 0 ? 1 : 0
  const jsonCount = outputs.includeJson ? 1 : 0
  const totalFiles = pngCount + htmlCount + jsonCount
  const fileExt = totalFiles === 1 ? (pngCount === 1 ? 'png' : htmlCount === 1 ? 'html' : 'json') : 'zip'
  const summaryParts: string[] = []
  if (pngCount > 0) summaryParts.push(`${pngCount} PNG${pngCount === 1 ? '' : 's'}`)
  if (htmlCount > 0) summaryParts.push('1 HTML')
  if (jsonCount > 0) summaryParts.push('1 JSON')
  return { pngCount, htmlCount, jsonCount, totalFiles, fileExt, summaryParts }
}

function cloneCartBlockConfig(blockConfig: ExportBlockConfig): ExportBlockConfig {
  switch (blockConfig.kind) {
    case 'summary':
      return { ...blockConfig, scope: { ...blockConfig.scope } }
    case 'details':
      return { ...blockConfig, scope: { ...blockConfig.scope }, selectedMetrics: [...blockConfig.selectedMetrics] }
    case 'waypoints':
      return { ...blockConfig, selectedColumns: [...blockConfig.selectedColumns] }
    case 'highlights':
      return { ...blockConfig, selectedCards: [...blockConfig.selectedCards] }
  }
}

async function executeStudioExport(
  expeditionId: number,
  exportData: ExpeditionExportData | null,
  cartItems: CartBlockItem[],
  outputPlan: ExportOutputPlan,
  statusStore: ExportStatusStore,
  setExporting: Dispatch<SetStateAction<boolean>>,
): Promise<void> {
  if (!exportData || outputPlan.totalFiles === 0) return
  setExporting(true)
  statusStore.clear()
  try {
    const expeditionSlug = slugify(exportData.plan.expedition.name)
    const singleFileSuffix = outputPlan.fileExt === 'png' ? cartItems[0].filenameSlug : outputPlan.fileExt === 'html' ? 'report' : 'snapshot'
    const defaultFilename = outputPlan.totalFiles === 1 ? `${expeditionSlug}-${singleFileSuffix}.${outputPlan.fileExt}` : `${expeditionSlug}-export.zip`
    const targetPath = await PickExportSavePath(defaultFilename, outputPlan.fileExt)
    if (!targetPath) return

    const bundleFiles: ExportBundleFile[] = []
    if (outputPlan.pngCount > 0) {
      statusStore.setStatus(`Rendering PNG 0 / ${cartItems.length}...`)
      const pngFiles = await renderCartToPngFiles(exportData, cartItems, (completedCount, totalCount) =>
        statusStore.setStatus(`Rendering PNG ${completedCount} / ${totalCount}...`),
      )
      bundleFiles.push(...pngFiles)
    }
    if (outputPlan.htmlCount > 0) {
      statusStore.setStatus('Building offline HTML report...')
      bundleFiles.push({ name: `${expeditionSlug}-report.html`, contentText: generateSelfContainedHtmlReport(exportData, cartItems) })
    }
    if (outputPlan.jsonCount > 0) {
      statusStore.setStatus('Serializing canonical JSON snapshot...')
      bundleFiles.push({ name: `${expeditionSlug}-snapshot.json`, canonicalJsonExpeditionId: expeditionId })
    }
    statusStore.setStatus('Writing export output...')
    const wireFiles = bundleFiles.map((file) => exporter.BundleFile.createFrom(file))
    const savedPath = await WriteExportOutput(targetPath, wireFiles)
    statusStore.setStatus(`Saved to ${savedPath}`)
  } catch (error: unknown) {
    statusStore.setError(error instanceof Error ? error.message : String(error))
  } finally {
    setExporting(false)
  }
}
