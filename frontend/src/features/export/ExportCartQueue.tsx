import { CartBlockItem } from './exportTypes'

/** Props for the compact ordered Export Cart queue panel. */
export interface ExportCartQueueProps {
  items: CartBlockItem[]
  inspectedCartItemId: string | null
  disabled?: boolean
  onApplyQuickShare: () => void
  onClearCart: () => void
  onInspectItem: (itemId: string) => void
  onMoveItem: (index: number, direction: -1 | 1) => void
  onRemoveItem: (index: number) => void
}

/**
 * ExportCartQueue renders the Quick Share preset trigger, Clear Cart button,
 * and compact reorderable list of queued export blocks.
 */
export function ExportCartQueue({
  items,
  inspectedCartItemId,
  disabled = false,
  onApplyQuickShare,
  onClearCart,
  onInspectItem,
  onMoveItem,
  onRemoveItem,
}: ExportCartQueueProps) {
  return (
    <div className="pt-3 border-t border-ed-border space-y-2">
      <div className="flex items-center justify-between gap-2">
        <span className="text-[10px] font-bold uppercase tracking-[0.14em] text-ed-text-muted">
          Export Cart ({items.length})
        </span>
        <div className="flex items-center gap-1.5">
          <button
            type="button"
            disabled={disabled}
            onClick={onApplyQuickShare}
            title="Populate cart with Quick Share preset (Summary, Highlights Top 1, Waypoints)"
            className={`px-2 py-0.5 text-[10px] font-bold uppercase tracking-wider border border-ed-amber/70 bg-ed-amber-tint text-ed-amber ${
              disabled ? 'opacity-40 cursor-not-allowed' : 'hover:bg-ed-amber hover:text-ed-void cursor-pointer'
            }`}
          >
            Quick Share
          </button>
          {items.length > 0 && (
            <button
              type="button"
              disabled={disabled}
              onClick={onClearCart}
              className={`px-2 py-0.5 text-[10px] font-bold uppercase tracking-wider border border-ed-border bg-ed-control text-ed-text-muted ${
                disabled ? 'opacity-40 cursor-not-allowed' : 'hover:text-ed-danger cursor-pointer'
              }`}
            >
              Clear
            </button>
          )}
        </div>
      </div>

      {items.length === 0 ? (
        <div className="p-3 border border-ed-border-subtle bg-ed-void text-[11px] text-ed-text-muted text-center">
          Cart is empty. Add blocks above or click Quick Share.
        </div>
      ) : (
        <div className="space-y-1 max-h-48 overflow-y-auto pr-1">
          {items.map((item, index) => {
            const isInspected = inspectedCartItemId === item.id
            return (
              <div
                key={item.id}
                className={`flex items-center gap-1.5 px-2 py-1.5 border text-[11px] ${
                  isInspected
                    ? 'bg-ed-cyan-tint border-ed-cyan text-ed-text-main'
                    : 'bg-ed-control border-ed-border text-ed-text-main'
                }`}
              >
                <button
                  type="button"
                  disabled={disabled}
                  onClick={() => onInspectItem(item.id)}
                  title="Preview this queued block"
                  className={`flex-1 min-w-0 text-left ${
                    disabled ? 'opacity-60 cursor-not-allowed' : 'cursor-pointer'
                  }`}
                >
                  <div className="font-bold truncate">
                    <span className="font-mono-tabular text-ed-text-muted mr-1.5">
                      {index + 1}.
                    </span>
                    {item.title}
                  </div>
                  <div className="text-[10px] text-ed-text-muted truncate">{item.subtitle}</div>
                </button>
                <div className="flex items-center gap-0.5 shrink-0 font-mono-tabular">
                  <button
                    type="button"
                    disabled={disabled || index === 0}
                    title="Move up"
                    onClick={() => onMoveItem(index, -1)}
                    className={`px-1 py-0.5 text-[9px] font-bold text-ed-text-muted ${
                      disabled || index === 0
                        ? 'opacity-30 cursor-not-allowed'
                        : 'hover:text-ed-amber cursor-pointer'
                    }`}
                  >
                    UP
                  </button>
                  <button
                    type="button"
                    disabled={disabled || index === items.length - 1}
                    title="Move down"
                    onClick={() => onMoveItem(index, 1)}
                    className={`px-1 py-0.5 text-[9px] font-bold text-ed-text-muted ${
                      disabled || index === items.length - 1
                        ? 'opacity-30 cursor-not-allowed'
                        : 'hover:text-ed-amber cursor-pointer'
                    }`}
                  >
                    DN
                  </button>
                  <button
                    type="button"
                    disabled={disabled}
                    title="Remove block"
                    onClick={() => onRemoveItem(index)}
                    className={`px-1 py-0.5 text-[9px] font-bold text-ed-text-muted ${
                      disabled ? 'opacity-30 cursor-not-allowed' : 'hover:text-ed-danger cursor-pointer'
                    }`}
                  >
                    X
                  </button>
                </div>
              </div>
            )
          })}
        </div>
      )}
    </div>
  )
}
