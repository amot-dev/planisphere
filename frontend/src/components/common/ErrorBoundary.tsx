import { Component, ErrorInfo, ReactNode } from 'react'
import { APP_PRODUCT_NAME, APP_VERSION } from '../../version'

/**
 * ErrorBoundaryProps defines the child subtree and optional custom fallback heading
 * for the cockpit subsystem error boundary.
 */
export interface ErrorBoundaryProps {
  children?: ReactNode
  fallbackTitle?: string
}

interface State {
  hasError: boolean
  error: Error | null
  errorInfo: ErrorInfo | null
}

/**
 * ErrorBoundary catches rendering exceptions in a UI subtree and displays
 * a recoverable cockpit subsystem exception screen instead of blanking the window.
 */
export class ErrorBoundary extends Component<ErrorBoundaryProps, State> {
  public state: State = {
    hasError: false,
    error: null,
    errorInfo: null,
  }

  public static getDerivedStateFromError(error: Error): State {
    return { hasError: true, error, errorInfo: null }
  }

  public componentDidCatch(error: Error, errorInfo: ErrorInfo) {
    console.error('[TerminalErrorBoundary caught error]:', error, errorInfo)
    this.setState({ errorInfo })
  }

  private handleReload = () => {
    if (typeof window !== 'undefined' && window.location) {
      window.location.reload()
    }
  }

  private handleReset = () => {
    this.setState({ hasError: false, error: null, errorInfo: null })
  }

  public render() {
    if (this.state.hasError) {
      return (
        <div className="h-full w-full min-h-[300px] flex flex-col items-center justify-center p-6 bg-[#050506] text-[#F4ECE4] select-none">
          <div className="w-full max-w-xl border border-[#4d1915] border-t-2 border-t-[#ff3b2f] bg-[#0b0907] p-6 space-y-4 shadow-2xl">
            <div className="flex items-center justify-between">
              <span className="text-xs font-extrabold tracking-[0.2em] uppercase text-[#ff3b2f]">
                {APP_PRODUCT_NAME} // SUBSYSTEM EXCEPTION
              </span>
              <span className="font-mono-tabular text-[11px] text-[#a6907c]">
                v{APP_VERSION}
              </span>
            </div>

            <div className="space-y-1">
              <div className="text-sm font-bold uppercase tracking-wider text-[#F4ECE4]">
                {this.props.fallbackTitle || 'Subsystem Failure Detected'}
              </div>
              <div className="text-xs text-[#a6907c]">
                The terminal encountered an unexpected runtime exception during rendering.
              </div>
            </div>

            {this.state.error && (
              <div className="bg-[#120909] border border-[#3d1412] p-3 text-xs font-mono-tabular text-[#ff8075] overflow-auto max-h-48 break-words select-text">
                <div className="font-bold">{this.state.error.name}: {this.state.error.message}</div>
                {this.state.error.stack && (
                  <pre className="text-[10px] text-[#a6907c] mt-2 whitespace-pre-wrap">
                    {this.state.error.stack}
                  </pre>
                )}
              </div>
            )}

            <div className="flex items-center justify-end gap-3 pt-2">
              <button
                type="button"
                onClick={this.handleReset}
                className="ed-btn"
              >
                Dismiss and Retry
              </button>
              <button
                type="button"
                onClick={this.handleReload}
                className="ed-btn-primary"
              >
                Reboot Terminal
              </button>
            </div>
          </div>
        </div>
      )
    }

    return this.props.children
  }
}

