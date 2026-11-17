package journal

import (
	"context"
	"log"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Bounds and default duration for live journal file event debouncing.
const (
	MinDebounceDelay     = 300 * time.Millisecond
	DefaultDebounceDelay = 2 * time.Second
	MaxDebounceDelay     = 30 * time.Second
	reconnectInterval    = 2 * time.Second
)

// ClampDebounceDelay restricts delay to [MinDebounceDelay, MaxDebounceDelay].
func ClampDebounceDelay(delay time.Duration) time.Duration {
	if delay < MinDebounceDelay {
		return MinDebounceDelay
	}
	if delay > MaxDebounceDelay {
		return MaxDebounceDelay
	}
	return delay
}

// Watcher tails the journal directory: fsnotify events trigger ingestion of
// the changed file (debounced).
type Watcher struct {
	ingester        *Ingester
	onBatch         func(Result)
	debounceNanos   atomic.Int64
	debounceChanged chan struct{}

	opMu      sync.Mutex // serializes Start and Stop lifecycle transitions
	mu        sync.Mutex
	dir       string
	cancel    context.CancelFunc
	done      chan struct{}
	active    bool
	lastError string
}

// NewWatcher creates a watcher initialized with DefaultDebounceDelay. onBatch
// is invoked (from the watcher goroutine) after every ingestion pass that
// inserted new events.
func NewWatcher(ingester *Ingester, onBatch func(Result)) *Watcher {
	watcher := &Watcher{
		ingester:        ingester,
		onBatch:         onBatch,
		debounceChanged: make(chan struct{}, 1),
	}
	watcher.debounceNanos.Store(int64(DefaultDebounceDelay))
	return watcher
}

// DebounceDelay returns the currently configured live file-event debounce duration.
func (watcher *Watcher) DebounceDelay() time.Duration {
	storedNanos := watcher.debounceNanos.Load()
	if storedNanos <= 0 {
		return DefaultDebounceDelay
	}
	return ClampDebounceDelay(time.Duration(storedNanos))
}

// SetDebounceDelay updates the live file-event debounce duration (clamped to
// [MinDebounceDelay, MaxDebounceDelay]) and reschedules any in-flight timer.
func (watcher *Watcher) SetDebounceDelay(delay time.Duration) {
	clampedDelay := ClampDebounceDelay(delay)
	watcher.debounceNanos.Store(int64(clampedDelay))
	select {
	case watcher.debounceChanged <- struct{}{}:
	default:
	}
}

// Active reports whether fsnotify is currently watching the directory.
func (watcher *Watcher) Active() (bool, string) {
	watcher.mu.Lock()
	defer watcher.mu.Unlock()
	return watcher.active, watcher.lastError
}

// Start (re)starts watching dir. Any previous watch is stopped.
func (watcher *Watcher) Start(parent context.Context, dir string) {
	watcher.opMu.Lock()
	defer watcher.opMu.Unlock()

	watcher.stopLocked()
	if parent != nil && parent.Err() != nil {
		return
	}
	if parent == nil {
		parent = context.Background()
	}
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
	watcher.opMu.Lock()
	defer watcher.opMu.Unlock()

	watcher.stopLocked()
}

func (watcher *Watcher) stopLocked() {
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
				debounceTimer.Reset(watcher.DebounceDelay())
			}
		case <-watcher.debounceChanged:
			if len(pending) > 0 {
				debounceTimer.Reset(watcher.DebounceDelay())
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
