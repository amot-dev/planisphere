declare global {
  interface Window {
    go?: {
      app?: {
        App?: {
          SetLiveSyncPaused?: (paused: boolean) => Promise<void>
        }
      }
    }
    runtime?: {
      EventsOnMultiple?: (
        eventName: string,
        callback: (...eventArgs: unknown[]) => void,
        maxCallbacks: number,
      ) => () => void
    }
  }
}

export {}

