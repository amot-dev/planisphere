package journal

import (
	"context"
	"log"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Watcher tails the journal directory: fsnotify events trigger ingestion of
// the changed file (debounced).
type Watcher struct {
	ingester *Ingester
	onBatch  func(Result)

	mu        sync.Mutex
	dir       string
	cancel    context.CancelFunc
	done      chan struct{}
	active    bool
	lastError string
}

// NewWatcher creates a watcher. onBatch is invoked (from the watcher
// goroutine) after every ingestion pass that inserted new events.
func NewWatcher(ingester *Ingester, onBatch func(Result)) *Watcher {
	return &Watcher{ingester: ingester, onBatch: onBatch}
}

// Active reports whether fsnotify is currently watching the directory.
func (watcher *Watcher) Active() (bool, string) {
	watcher.mu.Lock()
	defer watcher.mu.Unlock()
	return watcher.active, watcher.lastError
}

// Start (re)starts watching dir. Any previous watch is stopped.
func (watcher *Watcher) Start(parent context.Context, dir string) {
	watcher.Stop()
	ctx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	watcher.mu.Lock()
	watcher.dir = dir
	watcher.cancel = cancel
	watcher.done = done
	watcher.mu.Unlock()
	go watcher.run(ctx, dir, done)
}

// Stop stops watching.
func (watcher *Watcher) Stop() {
	watcher.mu.Lock()
	if watcher.cancel != nil {
		watcher.cancel()
		watcher.cancel = nil
	}
	done := watcher.done
	watcher.done = nil
	watcher.mu.Unlock()

	if done != nil {
		<-done
	}
	watcher.setActive(false, "")
}

func (watcher *Watcher) setActive(active bool, errorMessage string) {
	watcher.mu.Lock()
	watcher.active, watcher.lastError = active, errorMessage
	watcher.mu.Unlock()
}

const (
	debounceDelay     = 300 * time.Millisecond
	reconnectInterval = 2 * time.Second
)

func (watcher *Watcher) run(ctx context.Context, dir string, done chan struct{}) {
	var fsWatcher *fsnotify.Watcher
	var fsEvents <-chan fsnotify.Event
	var errorChan <-chan error

	tryWatch := func() {
		if fsWatcher != nil || !DirExists(dir) {
			return
		}
		newWatcher, watcherError := fsnotify.NewWatcher()
		if watcherError != nil {
			watcher.setActive(false, watcherError.Error())
			return
		}
		if addError := newWatcher.Add(dir); addError != nil {
			_ = newWatcher.Close()
			watcher.setActive(false, addError.Error())
			return
		}
		fsWatcher, fsEvents, errorChan = newWatcher, newWatcher.Events, newWatcher.Errors
		watcher.setActive(true, "")
	}
	defer func() {
		if fsWatcher != nil {
			_ = fsWatcher.Close()
		}
		watcher.setActive(false, "")
		close(done)
	}()

	tryWatch()
	pending := map[string]bool{}
	debounceTimer := time.NewTimer(time.Hour)
	defer debounceTimer.Stop()
	debounceTimer.Stop()

	reconnectTicker := time.NewTicker(reconnectInterval)
	defer reconnectTicker.Stop()

	flush := func(paths []string) {
		var total Result
		for _, path := range paths {
			fileResult, ingestError := watcher.ingester.IngestFile(path)
			if ingestError != nil {
				log.Printf("journal: ingest %s: %v", filepath.Base(path), ingestError)
			}
			total.merge(fileResult)
		}
		if total.NewEvents > 0 && watcher.onBatch != nil {
			watcher.onBatch(total)
		}
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-reconnectTicker.C:
			if fsWatcher == nil {
				tryWatch()
			}
		case fsEvent, ok := <-fsEvents:
			if !ok {
				if fsWatcher != nil {
					_ = fsWatcher.Close()
				}
				fsWatcher, fsEvents, errorChan = nil, nil, nil
				watcher.setActive(false, "watcher closed")
				continue
			}
			if IsJournalFile(fsEvent.Name) && fsEvent.Op&(fsnotify.Write|fsnotify.Create) != 0 {
				pending[fsEvent.Name] = true
				debounceTimer.Reset(debounceDelay)
			}
		case watchError, ok := <-errorChan:
			if ok && watchError != nil {
				log.Printf("journal: watcher error: %v", watchError)
			}
		case <-debounceTimer.C:
			paths := make([]string, 0, len(pending))
			for path := range pending {
				paths = append(paths, path)
			}
			pending = map[string]bool{}
			flush(paths)
		}
	}
}
