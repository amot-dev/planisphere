declare global {
  interface Window {
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

