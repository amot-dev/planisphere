import { memo } from 'react'
import { ExpeditionExportData, HighlightsBlockConfig } from '../exportTypes'
import { buildHighlightsViewModel, HighlightBoardViewModel } from '../exportViewModels'

/** Props for the self-contained Highlights trophy grid card component. */
export interface HighlightsBlockProps {
  exportData: ExpeditionExportData
  config: HighlightsBlockConfig
}

/**
 * HighlightsBlock renders opt-in trophy cards for standout legs and waypoints
 * at Top 1 (hero layout) or Top 3 / Top 5 (ranked leaderboard layout).
 */
export const HighlightsBlock = memo(function HighlightsBlock({
  exportData,
  config,
}: HighlightsBlockProps) {
  const viewModel = buildHighlightsViewModel(exportData, config)

  return (
    <div className="ed-exp-root">
      <div className="ed-exp-header ed-exp-header-cyan">
        <div>
          <div className="ed-exp-eyebrow">{viewModel.eyebrow}</div>
          <div className="ed-exp-title-row">
            <h2 className="ed-exp-title">{viewModel.expeditionName}</h2>
          </div>
        </div>
        <div className="ed-exp-dates ed-exp-mono">
          <div>{viewModel.trophyCountLabel}</div>
          <div className="ed-exp-muted">{viewModel.effectiveStartGalactic}</div>
        </div>
      </div>

      {viewModel.isEmpty ? (
        <div className="ed-exp-empty ed-exp-mono">{viewModel.emptyMessage}</div>
      ) : (
        <div className={`ed-exp-grid ed-exp-cols-${viewModel.gridCols}`}>
          {viewModel.boards.map((board) =>
            viewModel.depth === 1 ? (
              <HighlightHeroCard key={board.cardId} board={board} />
            ) : (
              <HighlightRankedCard key={board.cardId} board={board} />
            ),
          )}
        </div>
      )}

      <div className="ed-exp-footer ed-exp-mono">
        <span>PLANISPHERE // ASTROMETRIC TELEMETRY TERMINAL</span>
        <span>EXPORTED {viewModel.exportedAtGalactic}</span>
      </div>
    </div>
  )
})

function HighlightHeroCard({ board }: { board: HighlightBoardViewModel }) {
  const winner = board.entries[0]
  return (
    <div className={`ed-exp-tile ed-exp-top-${board.accent}`}>
      <div className="ed-exp-kpi-label">{board.title}</div>
      <div className={`ed-exp-hl-hero ed-exp-mono ed-exp-${board.accent}`}>
        {winner.formattedPrimary}
      </div>
      <div className="ed-exp-main" style={{ fontSize: '12px', marginTop: '4px' }}>
        {winner.label} · {winner.routeLabel}
        {winner.inSystem && (
          <span className="ed-exp-cyan ed-exp-mono" style={{ fontSize: '10px', marginLeft: '6px' }}>
            [IN SYSTEM]
          </span>
        )}
      </div>
      <div className="ed-exp-kpi-sub ed-exp-mono">{winner.secondaryLabel}</div>
    </div>
  )
}

function HighlightRankedCard({ board }: { board: HighlightBoardViewModel }) {
  return (
    <div className={`ed-exp-tile ed-exp-top-${board.accent}`}>
      <div className="ed-exp-kpi-label">{board.title}</div>
      {board.entries.map((entry) => (
        <div key={`${board.cardId}-${entry.rowId}-${entry.rank}`} className="ed-exp-hl-entry">
          <div className="ed-exp-hl-row">
            <span className="ed-exp-mono ed-exp-muted">
              #{entry.rank} {entry.label}
            </span>
            <strong className={`ed-exp-mono ed-exp-${board.accent}`}>{entry.formattedPrimary}</strong>
          </div>
          <div style={{ fontSize: '11px' }} className="ed-exp-main">
            {entry.routeLabel}
            {entry.inSystem && (
              <span className="ed-exp-cyan ed-exp-mono" style={{ fontSize: '10px', marginLeft: '6px' }}>
                [IN SYSTEM]
              </span>
            )}
          </div>
          <div className="ed-exp-kpi-sub ed-exp-mono">{entry.secondaryLabel}</div>
        </div>
      ))}
    </div>
  )
}
