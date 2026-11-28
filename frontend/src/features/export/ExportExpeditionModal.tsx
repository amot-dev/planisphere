import { ModalShell } from '../../components/ui/ModalShell'
import { SquareCheckbox } from '../../components/ui/SquareCheckbox'
import { DurationDisplayMode } from '../../domain/durationDisplay'
import { BlockConfigurator } from './BlockConfigurator'
import { EXPORT_CARD_CSS } from './exportCardStyles'
import { ExportCartQueue } from './ExportCartQueue'
import { ExportPreviewPane } from './ExportPreviewPane'
import { ExportOutputOptions } from './exportTypes'
import { ExportOutputPlan, ExportStudioState, useExportStudio } from './useExportStudio'

/** Props for the Export Studio modal dialog. */
export interface ExportExpeditionModalProps {
  expeditionId: number
  durationDisplayMode?: DurationDisplayMode
  onClose: () => void
}

const OUTPUT_FORMAT_TOGGLES: ReadonlyArray<{
  key: keyof ExportOutputOptions
  label: string
}> = [
  { key: 'includePng', label: 'PNG Images' },
  { key: 'includeHtml', label: 'HTML Report' },
  { key: 'includeJson', label: 'JSON Data' },
]

/**
 * ExportExpeditionModal hosts the split-screen Export Studio with sticky block
 * configuration, Quick Share preset, Live Preview, and PNG, HTML, and JSON export dispatch.
 */
export function ExportExpeditionModal({
  expeditionId,
  durationDisplayMode,
  onClose,
}: ExportExpeditionModalProps) {
  const studio = useExportStudio(expeditionId, durationDisplayMode)
  const { exportData, loading, exporting } = studio
  const subtitle = exportData
    ? `${exportData.plan.expedition.name} · ${exportData.plan.visitedWaypointCount} / ${exportData.plan.waypointCount} Waypoints Visited`
    : 'Loading expedition telemetry...'

  return (
    <ModalShell
      title="Export Studio"
      subtitle={subtitle}
      badge={exportData ? exportData.plan.expedition.status.toUpperCase() : undefined}
      accent="cyan"
      maxWidth="max-w-6xl"
      onClose={exporting ? undefined : onClose}
      closeDisabled={exporting}
      bodyClassName="flex-1 min-h-0 overflow-hidden flex flex-col"
      footer={
        <ExportStudioFooter
          outputs={studio.outputs}
          outputPlan={studio.outputPlan}
          exportReady={Boolean(exportData) && !loading}
          exporting={exporting}
          onToggleOutput={studio.handleToggleOutput}
          onClose={onClose}
          onRunExport={() => void studio.handleRunExport()}
        />
      }
    >
      <style>{EXPORT_CARD_CSS}</style>
      <ExportStudioBody studio={studio} />
    </ModalShell>
  )
}

function ExportStudioBody({ studio }: { studio: ExportStudioState }) {
  const { exportData, loading, loadError, exporting } = studio
  if (loading) {
    return <div className="p-10 text-center font-mono-tabular text-xs text-ed-amber uppercase tracking-widest">[ ASSEMBLING EXPEDITION TELEMETRY SNAPSHOT... ]</div>
  }
  if (loadError || !exportData) {
    return <div className="p-8 text-center text-xs text-ed-danger-text font-mono-tabular">{loadError || 'Unable to load expedition export snapshot.'}</div>
  }
  return (
    <div className="flex-1 min-h-0 flex flex-col lg:flex-row overflow-hidden">
      <div className="w-full lg:w-[360px] shrink-0 border-b lg:border-b-0 lg:border-r border-ed-border bg-ed-rail p-4 overflow-y-auto space-y-4">
        <BlockConfigurator
          exportData={exportData} config={studio.config} availableDetailsMetricIds={studio.availableDetailsMetricIds}
          addCount={studio.expandedPreviewItems.length} canAddToCart={studio.canAddToCart} disabled={exporting}
          onSelectBlockKind={studio.handleSelectBlockKind} onChangeConfig={studio.handleConfigChange} onAddToCart={studio.handleAddToCart}
        />
        <ExportCartQueue
          items={studio.cartItems} inspectedCartItemId={studio.inspectedCartItemId} disabled={exporting}
          onApplyQuickShare={studio.handleApplyQuickShare} onClearCart={studio.handleClearCart}
          onInspectItem={studio.handleInspectCartItem} onMoveItem={studio.handleMoveCartItem} onRemoveItem={studio.handleRemoveCartItem}
        />
      </div>
      <ExportPreviewPane
        exportData={exportData} expandedPreviewItems={studio.expandedPreviewItems} inspectedCartItem={studio.inspectedCartItem}
        disabled={exporting} statusStore={studio.statusStore} onExitInspectMode={studio.handleExitInspectMode}
      />
    </div>
  )
}

function ExportStudioFooter(props: {
  outputs: ExportOutputOptions
  outputPlan: ExportOutputPlan
  exportReady: boolean
  exporting: boolean
  onToggleOutput: (outputKey: keyof ExportOutputOptions) => void
  onClose: () => void
  onRunExport: () => void
}) {
  const { outputs, outputPlan, exportReady, exporting, onToggleOutput, onClose, onRunExport } = props
  const exportButtonDisabled = exporting || !exportReady || outputPlan.totalFiles === 0

  return (
    <div className="px-5 py-3 border-t border-ed-border bg-ed-header flex flex-wrap items-center justify-between gap-3 text-xs">
      <div className="flex flex-wrap items-center gap-4">
        {OUTPUT_FORMAT_TOGGLES.map((toggle) => (
          <button
            key={toggle.key}
            type="button"
            disabled={exporting}
            onClick={() => onToggleOutput(toggle.key)}
            className={`inline-flex items-center gap-2 text-ed-text-main ${exporting ? 'opacity-40 cursor-not-allowed' : 'cursor-pointer'}`}
          >
            <SquareCheckbox checked={outputs[toggle.key]} disabled={exporting} size="xs" />
            <span className="font-bold uppercase tracking-wider text-[11px]">{toggle.label}</span>
          </button>
        ))}
      </div>
      <div className="flex items-center gap-3">
        <span className="font-mono-tabular text-[11px] text-ed-text-muted">
          {outputPlan.totalFiles === 0 ? 'Select at least one output' : `${outputPlan.summaryParts.join(' + ')} -> .${outputPlan.fileExt}`}
        </span>
        <button type="button" disabled={exporting} onClick={onClose} className={`px-3 py-1.5 text-[11px] font-bold uppercase tracking-wider border border-ed-border bg-ed-control text-ed-text-muted ${exporting ? 'opacity-40 cursor-not-allowed' : 'hover:text-ed-text-main cursor-pointer'}`}>
          Close
        </button>
        <button type="button" disabled={exportButtonDisabled} onClick={onRunExport} className={`ed-btn-primary ${exportButtonDisabled ? 'opacity-40 cursor-not-allowed' : ''}`}>
          {exporting ? 'EXPORTING...' : 'EXPORT'}
        </button>
      </div>
    </div>
  )
}
