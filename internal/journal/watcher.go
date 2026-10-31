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
	ing     *Ingester
	onBatch func(Result)

	mu      sync.Mutex
	dir     string
	cancel  context.CancelFunc
	active  bool
	lastErr string
}

// NewWatcher creates a watcher. onBatch is invoked (from the watcher
// goroutine) after every ingestion pass that inserted new events.
func NewWatcher(ing *Ingester, onBatch func(Result)) *Watcher {
	return &Watcher{ing: ing, onBatch: onBatch}
}

// Active reports whether fsnotify is currently watching the directory.
func (w *Watcher) Active() (bool, string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.active, w.lastErr
}

// Start (re)starts watching dir. Any previous watch is stopped.
func (w *Watcher) Start(parent context.Context, dir string) {
	w.Stop()
	ctx, cancel := context.WithCancel(parent)
	w.mu.Lock()
	w.dir = dir
	w.cancel = cancel
	w.mu.Unlock()
	go w.run(ctx, dir)
}

// Stop stops watching.
func (w *Watcher) Stop() {
	w.mu.Lock()
	if w.cancel != nil {
		w.cancel()
		w.cancel = nil
	}
	w.active = false
	w.mu.Unlock()
}

func (w *Watcher) setActive(active bool, errMsg string) {
	w.mu.Lock()
	w.active, w.lastErr = active, errMsg
	w.mu.Unlock()
}

const (
	debounceDelay = 300 * time.Millisecond
	pollInterval  = 3 * time.Second
)

func (w *Watcher) run(ctx context.Context, dir string) {
	var fw *fsnotify.Watcher
	var events <-chan fsnotify.Event
	var errs <-chan error

	tryWatch := func() {
		if fw != nil || !DirExists(dir) {
			return
		}
		nw, err := fsnotify.NewWatcher()
		if err != nil {
			w.setActive(false, err.Error())
			return
		}
		if err := nw.Add(dir); err != nil {
			nw.Close()
			w.setActive(false, err.Error())
			return
		}
		fw, events, errs = nw, nw.Events, nw.Errors
		w.setActive(true, "")
	}
	defer func() {
		if fw != nil {
			fw.Close()
		}
		w.setActive(false, "")
	}()

	tryWatch()
	pending := map[string]bool{}
	debounce := time.NewTimer(time.Hour)
	debounce.Stop()
	poll := time.NewTicker(pollInterval)
	defer poll.Stop()

	flush := func(paths []string) {
		var total Result
		for _, p := range paths {
			r, err := w.ing.IngestFile(p)
			if err != nil {
				log.Printf("journal: ingest %s: %v", filepath.Base(p), err)
			}
			total.merge(r)
		}
		if len(total.NewEvents) > 0 && w.onBatch != nil {
			w.onBatch(total)
		}
	}

	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-events:
			if !ok {
				fw, events, errs = nil, nil, nil
				w.setActive(false, "watcher closed")
				continue
			}
			if IsJournalFile(ev.Name) && ev.Op&(fsnotify.Write|fsnotify.Create) != 0 {
				pending[ev.Name] = true
				debounce.Reset(debounceDelay)
			}
		case err, ok := <-errs:
			if ok && err != nil {
				log.Printf("journal: watcher error: %v", err)
			}
		case <-debounce.C:
			paths := make([]string, 0, len(pending))
			for p := range pending {
				paths = append(paths, p)
			}
			pending = map[string]bool{}
			flush(paths)
		case <-poll.C:
			tryWatch()
			files, err := ListJournalFiles(dir)
			if err != nil || len(files) == 0 {
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
