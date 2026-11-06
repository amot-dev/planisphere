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
// the changed file (debounced), and a slow poll of the newest journal acts as
// a safety net for platforms/filesystems (Proton, network shares, Windows
// buffered writes) where change notifications are unreliable.
type Watcher struct {
	ingester *Ingester
	onBatch  func(Result)

	mu      sync.Mutex
	dir     string
	cancel  context.CancelFunc
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
	watcher.mu.Lock()
	watcher.dir = dir
	watcher.cancel = cancel
	watcher.mu.Unlock()
	go watcher.run(ctx, dir)
}

// Stop stops watching.
func (watcher *Watcher) Stop() {
	watcher.mu.Lock()
	if watcher.cancel != nil {
		watcher.cancel()
		watcher.cancel = nil
	}
	watcher.active = false
	watcher.mu.Unlock()
}

func (watcher *Watcher) setActive(active bool, errorMessage string) {
	watcher.mu.Lock()
	watcher.active, watcher.lastError = active, errorMessage
	watcher.mu.Unlock()
}

const (
	debounceDelay = 300 * time.Millisecond
	pollInterval  = 3 * time.Second
)

func (watcher *Watcher) run(ctx context.Context, dir string) {
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
			newWatcher.Close()
			watcher.setActive(false, addError.Error())
			return
		}
		fsWatcher, fsEvents, errorChan = newWatcher, newWatcher.Events, newWatcher.Errors
		watcher.setActive(true, "")
	}
	defer func() {
		if fsWatcher != nil {
			fsWatcher.Close()
		}
		watcher.setActive(false, "")
	}()

	tryWatch()
	pending := map[string]bool{}
	debounce := time.NewTimer(time.Hour)
	defer debounce.Stop()
	debounce.Stop()
	poll := time.NewTicker(pollInterval)
	defer poll.Stop()

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
		case fsEvent, ok := <-fsEvents:
			if !ok {
				fsWatcher, fsEvents, errorChan = nil, nil, nil
				watcher.setActive(false, "watcher closed")
				continue
			}
			if IsJournalFile(fsEvent.Name) && fsEvent.Op&(fsnotify.Write|fsnotify.Create) != 0 {
				pending[fsEvent.Name] = true
				debounce.Reset(debounceDelay)
			}
		case watchError, ok := <-errorChan:
			if ok && watchError != nil {
				log.Printf("journal: watcher error: %v", watchError)
			}
		case <-debounce.C:
			paths := make([]string, 0, len(pending))
			for path := range pending {
				paths = append(paths, path)
			}
			pending = map[string]bool{}
			flush(paths)
		case <-poll.C:
			tryWatch()
			files, listError := ListJournalFiles(dir)
			if listError != nil || len(files) == 0 {
				continue
			}
			// The newest journal (and the previous one, in case the game
			// rolled over between polls) are the only files that can grow.
			start := len(files) - 2
			if start < 0 {
				start = 0
			}
			flush(files[start:])
		}
	}
}
