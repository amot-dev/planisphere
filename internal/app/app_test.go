package app

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"planisphere/internal/expedition"
)

func TestDatabaseHotSwapConcurrent(t *testing.T) {
	tempDir := t.TempDir()
	db1Path := filepath.Join(tempDir, "db1.sqlite")
	db2Path := filepath.Join(tempDir, "db2.sqlite")

	testApp := New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := testApp.initialize(ctx, db1Path); err != nil {
		t.Fatalf("initialize db1: %v", err)
	}
	defer testApp.shutdown(ctx)

	var wg sync.WaitGroup
	stop := make(chan struct{})

	// Goroutine 1: Continuous GetSettings
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				_, _ = testApp.GetSettings()
				time.Sleep(1 * time.Millisecond)
			}
		}
	}()

	// Goroutine 2: Continuous GetModesState
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				_, _ = testApp.GetModesState()
				time.Sleep(1 * time.Millisecond)
			}
		}
	}()

	// Goroutine 3: Continuous ListExpeditions
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				_, _ = testApp.ListExpeditions()
				time.Sleep(1 * time.Millisecond)
			}
		}
	}()

	// Goroutine 4: Continuous GetIngestStatus
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				_, _ = testApp.GetIngestStatus()
				time.Sleep(1 * time.Millisecond)
			}
		}
	}()

	// Goroutine 5: Perform hot swaps back and forth between db1 and db2
	for i := 0; i < 5; i++ {
		targetPath := db2Path
		if i%2 == 0 {
			targetPath = db1Path
		} else {
			targetPath = filepath.Join(tempDir, fmt.Sprintf("db_%d.sqlite", i))
		}
		if err := testApp.openAndWireDB(targetPath); err != nil {
			t.Errorf("hot-swap to %s failed: %v", targetPath, err)
		}
		time.Sleep(10 * time.Millisecond)
	}

	close(stop)
	wg.Wait()
}

func TestResolveRowSync_ContextCancellation(t *testing.T) {
	testApp := New()
	ctx, cancel := context.WithCancel(context.Background())
	testApp.ctx = ctx
	testApp.cancel = cancel

	// Fill semaphore so resolveRowSync has to wait
	testApp.resolveSem <- struct{}{}

	// Cancel context
	cancel()

	done := make(chan struct{})
	go func() {
		resolved, updated := testApp.resolveRowSync(expedition.Row{ID: 1, SystemName: "Sol"}, true)
		if resolved || updated {
			t.Errorf("expected false, false, got %v, %v", resolved, updated)
		}
		close(done)
	}()

	select {
	case <-done:
		// success: returned promptly due to ctx cancellation
	case <-time.After(2 * time.Second):
		t.Fatal("resolveRowSync blocked waiting for semaphore after context was cancelled")
	}
}

func TestResolveRowSync_SemaphoreReleased(t *testing.T) {
	testApp := New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	testApp.ctx = ctx
	testApp.cancel = cancel

	// resolveRowSync with online=true, but resolver is nil
	// Should acquire semaphore and release it via defer
	resolved, updated := testApp.resolveRowSync(expedition.Row{ID: 1, SystemName: "Sol"}, true)
	if resolved || updated {
		t.Errorf("expected false, false on nil db/resolver, got %v, %v", resolved, updated)
	}

	// Verify semaphore is available (can acquire without blocking)
	select {
	case testApp.resolveSem <- struct{}{}:
		// Semaphore token was successfully released!
		<-testApp.resolveSem
	default:
		t.Fatal("semaphore was not released by resolveRowSync")
	}
}

func TestMoveRow_NonexistentRow(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.sqlite")

	testApp := New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := testApp.initialize(ctx, dbPath); err != nil {
		t.Fatalf("initialize db: %v", err)
	}
	defer testApp.shutdown(ctx)

	err := testApp.MoveRow(99999, 0)
	if err == nil {
		t.Fatalf("expected error when moving nonexistent row, got nil")
	}
}

