import { useCallback, useEffect, useRef, useState, type KeyboardEvent } from 'react'
import { SearchWaypoints } from '../../../api/backend'
import { expedition } from '../../../api/models'
import { SearchIcon } from '../../../components/ui/HudIcons'
import { NonWaypointVisitsCard, WaypointSearchResultCard } from './WaypointSearchCards'

const SEARCH_DEBOUNCE_MS = 120

/**
 * WaypointSearchModalProps defines optional prefilled query text, active
 * expedition identity, dismissal callback, and navigation handlers.
 */
export interface WaypointSearchModalProps {
  initialQuery?: string
  activeExpeditionId: number
  onClose: () => void
  onNavigateToWaypoint: (expeditionId: number, rowId: number) => void
  onSelectExpedition: (expeditionId: number) => void
}

/**
 * WaypointSearchModal renders the command-palette modal for searching waypoint
 * systems and POIs across all expeditions alongside exact-system journal visits.
 */
export function WaypointSearchModal({
  initialQuery,
  activeExpeditionId,
  onClose,
  onNavigateToWaypoint,
  onSelectExpedition,
}: WaypointSearchModalProps) {
  const [query, setQuery] = useState(() => initialQuery ?? '')
  const [response, setResponse] = useState<expedition.SearchResponse | null>(null)
  const [resolvedQuery, setResolvedQuery] = useState('')
  const [loading, setLoading] = useState(false)
  const [errorMessage, setErrorMessage] = useState('')
  const [selectedIndex, setSelectedIndex] = useState(0)

  const inputRef = useRef<HTMLInputElement>(null)
  const listboxRef = useRef<HTMLDivElement>(null)
  const requestSequenceRef = useRef(0)
  const hasUserEditedRef = useRef(false)
  const onCloseRef = useRef(onClose)
  onCloseRef.current = onClose

  const trimmedQuery = query.trim()

  useEffect(() => {
    inputRef.current?.focus()
    const handleGlobalKeyDown = (event: globalThis.KeyboardEvent) => {
      if (event.key === 'Escape') {
        event.preventDefault()
        onCloseRef.current()
      }
    }
    window.addEventListener('keydown', handleGlobalKeyDown)
    return () => window.removeEventListener('keydown', handleGlobalKeyDown)
  }, [])

  useEffect(() => {
    if (!trimmedQuery) {
      requestSequenceRef.current += 1
      setResponse(null)
      setResolvedQuery('')
      setLoading(false)
      setErrorMessage('')
      setSelectedIndex(0)
      return
    }

    const executeImmediately = !hasUserEditedRef.current
    const currentSequence = requestSequenceRef.current + 1
    requestSequenceRef.current = currentSequence
    let cancelled = false
    setLoading(true)
    setErrorMessage('')

    const runQuery = () => {
      SearchWaypoints(trimmedQuery)
        .then((searchResponse) => {
          if (cancelled || requestSequenceRef.current !== currentSequence) return
          setResponse(searchResponse)
          setResolvedQuery(trimmedQuery)
          setSelectedIndex(0)
          setLoading(false)
        })
        .catch((searchError) => {
          if (cancelled || requestSequenceRef.current !== currentSequence) return
          setResponse(null)
          setResolvedQuery('')
          setSelectedIndex(0)
          setErrorMessage(String(searchError))
          setLoading(false)
        })
    }

    if (executeImmediately) {
      runQuery()
      return () => {
        cancelled = true
      }
    }

    const debounceTimerId = window.setTimeout(runQuery, SEARCH_DEBOUNCE_MS)
    return () => {
      cancelled = true
      window.clearTimeout(debounceTimerId)
    }
  }, [trimmedQuery])

  const results = response?.results ?? []
  const nonWaypointVisits = response?.nonWaypointVisits?.visits?.length
    ? response.nonWaypointVisits
    : null

  const handleSelectAttributedExpedition = useCallback(
    (expeditionId: number) => {
      onSelectExpedition(expeditionId)
      onClose()
    },
    [onClose, onSelectExpedition],
  )

  const handleDialogKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    if (results.length === 0) return
    const scrollOptionIntoView = (optionIndex: number) => {
      listboxRef.current
        ?.querySelector<HTMLElement>(`[data-search-option-index="${optionIndex}"]`)
        ?.scrollIntoView?.({ block: 'nearest' })
    }
    if (event.key === 'ArrowDown') {
      event.preventDefault()
      const nextIndex = Math.min(selectedIndex + 1, results.length - 1)
      setSelectedIndex(nextIndex)
      scrollOptionIntoView(nextIndex)
    } else if (event.key === 'ArrowUp') {
      event.preventDefault()
      const nextIndex = Math.max(selectedIndex - 1, 0)
      setSelectedIndex(nextIndex)
      scrollOptionIntoView(nextIndex)
    } else if (event.key === 'Enter') {
      if (loading || errorMessage || resolvedQuery !== trimmedQuery) return
      if ((event.target as HTMLElement | null)?.tagName === 'BUTTON') return
      const activeResult = results[selectedIndex]
      const firstStop = activeResult?.stops?.[0]
      if (activeResult && firstStop) {
        event.preventDefault()
        onNavigateToWaypoint(activeResult.expeditionId, firstStop.rowId)
      }
    }
  }

  return (
    <div
      onClick={onClose}
      className="fixed inset-0 z-50 flex items-start justify-center pt-[10vh] bg-black/80 p-4"
    >
      <div
        role="dialog"
        aria-modal="true"
        aria-label="Search waypoints and visit history"
        onClick={(event) => event.stopPropagation()}
        onKeyDown={handleDialogKeyDown}
        className="w-full max-w-2xl max-h-[80vh] flex flex-col border border-ed-border border-t-2 border-t-ed-amber bg-ed-panel shadow-2xl overflow-hidden"
      >
        <div className="relative px-4 py-3 border-b border-ed-border bg-ed-header flex items-center gap-2.5">
          <span className="text-ed-amber shrink-0">
            <SearchIcon />
          </span>
          <input
            ref={inputRef}
            type="text"
            spellCheck={false}
            value={query}
            onChange={(event) => {
              hasUserEditedRef.current = true
              setQuery(event.target.value)
            }}
            placeholder="Search star systems or POIs across expeditions..."
            aria-label="Search waypoints and visit history"
            className="flex-1 min-w-0 bg-transparent text-sm font-mono-tabular text-ed-text-main placeholder:text-ed-text-dim focus:outline-none"
          />
          {query && (
            <button
              type="button"
              onClick={() => {
                hasUserEditedRef.current = true
                setQuery('')
                inputRef.current?.focus()
              }}
              className="px-1.5 py-0.5 text-[10px] font-mono-tabular uppercase tracking-wider text-ed-text-muted hover:text-ed-text-main border border-ed-border bg-ed-control hover:bg-ed-hover cursor-pointer shrink-0"
            >
              CLEAR
            </button>
          )}
          <button
            type="button"
            onClick={onClose}
            aria-label="Close search"
            title="Close search"
            className="px-2 py-1 text-ed-text-muted hover:text-ed-text-main hover:bg-ed-hover cursor-pointer shrink-0"
          >
            <span>✕</span>
          </button>
          {loading && (
            <div className="absolute bottom-0 left-0 right-0 h-[2px] bg-ed-elevated overflow-hidden">
              <div className="w-1/3 h-full bg-ed-amber ed-scan-bar" />
            </div>
          )}
        </div>

        <div className="flex-1 overflow-y-auto p-4 space-y-3 text-xs">
          {errorMessage && (
            <div className="p-2.5 border border-ed-danger bg-ed-danger-tint text-ed-danger-text">
              {errorMessage}
            </div>
          )}

          {!trimmedQuery && (
            <div className="py-8 text-center text-ed-text-muted">
              Type a star system or POI name to search across expeditions.
            </div>
          )}

          {trimmedQuery && loading && !response && !errorMessage && (
            <div className="py-8 text-center font-mono-tabular uppercase tracking-wider text-ed-text-muted">
              Searching expeditions and journal visits...
            </div>
          )}

          {trimmedQuery && !loading && !errorMessage && results.length === 0 && !nonWaypointVisits && (
            <div className="py-8 text-center text-ed-text-muted">
              No matching waypoints or journal visits found.
            </div>
          )}

          {results.length > 0 && (
            <div ref={listboxRef} role="listbox" aria-label="Matching waypoints" className="space-y-1.5">
              {results.map((searchResult, resultIndex) => {
                const firstStopRowId = searchResult.stops?.[0]?.rowId ?? resultIndex
                return (
                  <WaypointSearchResultCard
                    key={`${searchResult.expeditionId}-${firstStopRowId}`}
                    result={searchResult}
                    resultIndex={resultIndex}
                    isCurrentExpedition={searchResult.expeditionId === activeExpeditionId}
                    isSelected={resultIndex === selectedIndex}
                    onHoverSelect={setSelectedIndex}
                    onNavigateToWaypoint={onNavigateToWaypoint}
                  />
                )
              })}
            </div>
          )}

          {response && response.totalMatches > results.length && (
            <div className="pt-1 text-center font-mono-tabular text-[11px] text-ed-text-muted">
              {`Showing ${results.length} of ${response.totalMatches} matches - refine query to narrow`}
            </div>
          )}

          {nonWaypointVisits && (
            <NonWaypointVisitsCard
              key={nonWaypointVisits.systemName}
              summary={nonWaypointVisits}
              onSelectExpedition={handleSelectAttributedExpedition}
            />
          )}
        </div>
      </div>
    </div>
  )
}
