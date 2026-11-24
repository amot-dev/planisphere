import {
  computeAllHighlightLeaderboards,
  HighlightLeaderboard,
  selectHighlightCards,
} from '../exportHighlights'
import { ExpeditionExportData, HighlightsBlockConfig } from '../exportTypes'

/** Props for the self-contained Highlights trophy grid card component. */
export interface HighlightsBlockProps {
  exportData: ExpeditionExportData
  config: HighlightsBlockConfig
}

/**
 * HighlightsBlock renders opt-in trophy cards for standout legs and waypoints
 * at Top 1 (hero layout) or Top 3 / Top 5 (ranked leaderboard layout).
 */
export function HighlightsBlock({ exportData, config }: HighlightsBlockProps) {
  const allLeaderboards = computeAllHighlightLeaderboards(exportData, {
    lucrativeMetric: config.lucrativeMetric,
    distanceMetric: config.distanceMetric,
    cardIds: config.selectedCards,
    maxDepth: config.depth,
  })
  const visibleBoards = selectHighlightCards(allLeaderboards, config.selectedCards, config.depth)
  const colsClass =
    visibleBoards.length <= 1
      ? 'ed-exp-cols-1'
      : visibleBoards.length === 2 || visibleBoards.length === 4
        ? 'ed-exp-cols-2'
        : 'ed-exp-cols-3'

  return (
    <div className="ed-exp-root">
      <div className="ed-exp-header ed-exp-header-cyan">
        <div>
          <div className="ed-exp-eyebrow">EXPEDITION HIGHLIGHTS // TOP {config.depth}</div>
          <div className="ed-exp-title-row">
            <h2 className="ed-exp-title">{exportData.plan.expedition.name}</h2>
          </div>
        </div>
        <div className="ed-exp-dates ed-exp-mono">
          <div>{visibleBoards.length} TROPHY CARDS</div>
          <div className="ed-exp-muted">{exportData.effectiveStartGalactic}</div>
        </div>
      </div>

      {visibleBoards.length === 0 ? (
        <div className="ed-exp-empty ed-exp-mono">
          Select at least one highlight card with recorded expedition data
        </div>
      ) : (
        <div className={`ed-exp-grid ${colsClass}`}>
          {visibleBoards.map((board) => (
            <HighlightTrophyCard key={board.cardId} board={board} depth={config.depth} />
          ))}
        </div>
      )}

      <div className="ed-exp-footer ed-exp-mono">
        <span>PLANISPHERE // ASTROMETRIC TELEMETRY TERMINAL</span>
        <span>EXPORTED {exportData.exportedAtGalactic}</span>
      </div>
    </div>
  )
}

function HighlightTrophyCard({
  board,
  depth,
}: {
  board: HighlightLeaderboard
  depth: 1 | 3 | 5
}) {
  const topClass = `ed-exp-top-${board.accent}`
  const accentTextClass = `ed-exp-${board.accent}`

  if (depth === 1) {
    const winner = board.entries[0]
    return (
      <div className={`ed-exp-tile ${topClass}`}>
        <div className="ed-exp-kpi-label">{board.title}</div>
        <div className={`ed-exp-hl-hero ed-exp-mono ${accentTextClass}`}>
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

  return (
    <div className={`ed-exp-tile ${topClass}`}>
      <div className="ed-exp-kpi-label">{board.title}</div>
      {board.entries.map((entry) => (
        <div key={`${board.cardId}-${entry.rowId}-${entry.rank}`} className="ed-exp-hl-entry">
          <div className="ed-exp-hl-row">
            <span className="ed-exp-mono ed-exp-muted">
              #{entry.rank} {entry.label}
            </span>
            <strong className={`ed-exp-mono ${accentTextClass}`}>{entry.formattedPrimary}</strong>
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

