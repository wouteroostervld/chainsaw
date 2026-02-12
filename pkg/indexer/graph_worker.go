package indexer

import (
	"context"
	"log/slog"
	"time"
)

// GraphWorker continuously processes chunks needing graph extraction
type GraphWorker struct {
	indexer      *Indexer
	pollInterval time.Duration
	batchSize    int
	concurrency  int
	stopCh       chan struct{}
}

// GraphWorkerConfig configures the graph extraction worker
type GraphWorkerConfig struct {
	PollInterval time.Duration // How often to check for new chunks
	BatchSize    int           // Chunks to fetch per poll
	Concurrency  int           // Number of parallel LLM requests
}

// NewGraphWorker creates a new graph extraction worker
func NewGraphWorker(indexer *Indexer, cfg GraphWorkerConfig) *GraphWorker {
	if cfg.PollInterval == 0 {
		cfg.PollInterval = 5 * time.Second
	}
	if cfg.BatchSize == 0 {
		cfg.BatchSize = 150 // Fetch up to 150 chunks at once
	}
	if cfg.Concurrency == 0 {
		cfg.Concurrency = 1 // Process 1 batch at a time by default
	}

	return &GraphWorker{
		indexer:      indexer,
		pollInterval: cfg.PollInterval,
		batchSize:    cfg.BatchSize,
		concurrency:  cfg.Concurrency,
		stopCh:       make(chan struct{}),
	}
}

// Start begins the graph extraction worker loop
func (gw *GraphWorker) Start(ctx context.Context) {
	slog.Info("Graph worker started",
		"poll_interval", gw.pollInterval,
		"batch_size", gw.batchSize,
		"concurrency", gw.concurrency)

	ticker := time.NewTicker(gw.pollInterval)
	defer ticker.Stop()

	// Do initial check immediately
	gw.processChunks(ctx)

	for {
		select {
		case <-ctx.Done():
			slog.Info("Graph worker stopping (context canceled)")
			return
		case <-gw.stopCh:
			slog.Info("Graph worker stopping (stop signal)")
			return
		case <-ticker.C:
			gw.processChunks(ctx)
		}
	}
}

// Stop signals the worker to stop
func (gw *GraphWorker) Stop() {
	close(gw.stopCh)
}

// processChunks fetches unextracted chunks and processes them
func (gw *GraphWorker) processChunks(ctx context.Context) {
	// Get chunks needing extraction
	chunkIDs, err := gw.indexer.db.GetChunksNeedingGraphExtraction(gw.batchSize)
	if err != nil {
		slog.Error("Failed to get chunks needing extraction", "error", err)
		return
	}

	if len(chunkIDs) == 0 {
		// No work to do
		return
	}

	slog.Info("Processing graph extraction batch",
		"chunk_count", len(chunkIDs))

	// Process the batch
	edgeCount, err := gw.indexer.ProcessGraphBatch(ctx, chunkIDs)
	if err != nil {
		slog.Error("Graph extraction batch failed",
			"error", err,
			"chunk_count", len(chunkIDs))
		// Don't mark as extracted if it failed
		return
	}

	// Mark chunks as extracted
	if err := gw.indexer.db.MarkChunksGraphExtracted(chunkIDs); err != nil {
		slog.Error("Failed to mark chunks as extracted",
			"error", err,
			"chunk_count", len(chunkIDs))
		return
	}

	slog.Info("Graph extraction batch completed",
		"chunks_processed", len(chunkIDs),
		"edges_created", edgeCount)

	// Get stats
	total, extracted, pending, err := gw.indexer.db.GetGraphExtractionStats()
	if err == nil && pending > 0 {
		slog.Info("Graph extraction progress",
			"total_chunks", total,
			"extracted", extracted,
			"pending", pending)
	}
}
