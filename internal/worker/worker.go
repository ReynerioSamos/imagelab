package worker

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"imagelab/internal/data"

	"golang.org/x/image/draw"
)

type Worker struct {
	models             data.Models
	storageRoot        string
	logger             *slog.Logger
	delay              time.Duration // artificial per-variant delay; 0 in normal operation
	quietDelay         time.Duration // Condition A quiet delay
	simulationInterval time.Duration // optional interval for simulated progress updates
	wg                 sync.WaitGroup
	notify             func(jobID string) // callback to invoke globalHub.NotifyAll after commit
}

// New constructs a Worker. delay is applied once per variant while a job
// is being generated, purely to make queueing and processing duration
// observable during measurement (Week 4's five-image burst test). It
// must be 0 for any timing meant to reflect real work, and is wired to
// -worker-delay in cmd/api/main.go rather than hardcoded -- the same
// pattern -report-delay used in the earlier synchronous/async lab.
//
// Test 2 - Updated to accept or set up notification callback
func New(models data.Models, storageRoot string, logger *slog.Logger, delay time.Duration, quietDelay time.Duration, simulationInterval time.Duration, notify func(jobID string)) *Worker {
	return &Worker{
		models:             models,
		storageRoot:        storageRoot,
		logger:             logger,
		delay:              delay,
		quietDelay:         quietDelay,
		simulationInterval: simulationInterval,
		notify:             notify,
	}
}

func (w *Worker) Start(ctx context.Context) {
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				w.logger.Info("worker shutting down")
				return
			case <-ticker.C:
				w.processNextJob(ctx)
			}
		}
	}()
}

func (w *Worker) Stop() {
	w.wg.Wait()
}

func (w *Worker) processNextJob(ctx context.Context) {
	job, err := w.models.Jobs.ClaimNext()
	if err != nil {
		w.logger.Error("failed to claim next job", "error", err)
		return
	}
	if job == nil {
		return // No job available
	}

	w.logger.Info("processing job", "job_id", job.ID, "image_id", job.ImageID)

	if err := w.executeJob(ctx, job); err != nil {
		if errors.Is(err, context.Canceled) {
			w.logger.Info("job processing interrupted by shutdown", "job_id", job.ID)
			return
		}
		w.logger.Error("job processing failed", "job_id", job.ID, "error", err)
		
		// Mark failed in a transaction with version bump
		tx, txErr := w.models.DB.BeginTx(ctx, nil)
		if txErr == nil {
			_, _ = w.models.Jobs.MarkFailedTx(ctx, tx, job.ID, "processing failed due to server image transform error")
			_ = tx.Commit()
			if w.notify != nil {
				w.notify(job.ID)
			}
		}
		return
	}

	w.logger.Info("job completed successfully", "job_id", job.ID)
}

// builtVariant is a fully generated and encoded variant that has not yet
// touched the filesystem or database. Keeping every variant in memory
// until all three succeed is what makes IMG-03's "all-or-complete"
// contract real: nothing is persisted until all three are ready together.
type builtVariant struct {
	name           string
	storedFilename string
	bytes          []byte
	width, height  int
}

func (w *Worker) executeJob(ctx context.Context, job *data.Job) error {
	imgRecord, err := w.models.Images.Get(job.ImageID)
	if err != nil {
		return fmt.Errorf("failed to get image record: %w", err)
	}

	srcPath := filepath.Join(w.storageRoot, "originals", imgRecord.StoredFilename)
	srcFile, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("failed to open original file: %w", err)
	}
	defer srcFile.Close()

	srcImg, format, err := image.Decode(srcFile)
	if err != nil {
		return fmt.Errorf("failed to decode image: %w", err)
	}

	// --- CONDITION A: Quiet Job (30s delay with no intermediate updates) ---
	if w.quietDelay > 0 {
		w.logger.Info("applying condition A quiet delay", "duration", w.quietDelay)
		time.Sleep(w.quietDelay)
	}

	// --- CONDITION C1 / C2 / C3: Simulated Update Phase ---
	if w.simulationInterval > 0 {
		for step := 1; step <= 5; step++ {
			time.Sleep(w.simulationInterval)
			tx, err := w.models.DB.BeginTx(ctx, nil)
			if err != nil {
				return err
			}
			stageMsg := fmt.Sprintf("Simulated progress (step %d/5)", step)
			newVersion, updateErr := w.models.Jobs.UpdateProgressTx(ctx, tx, job.ID, stageMsg, 0)
			if updateErr != nil {
				tx.Rollback()
				return updateErr
			}
			job.Version = newVersion // update job version
			if err := tx.Commit(); err != nil {
				return err
			}

			w.logger.Info("job version committed",
				"job_id", job.ID,
				"version", job.Version,
				"status", job.Status,
				"stage", stageMsg,
				"commit_time", time.Now().UTC().Format(time.RFC3339Nano),
			)

			if w.notify != nil {
				w.notify(job.ID)
			}
		}
	}

	targets := []struct {
		name       string
		maxW       int
		maxH       int
		cropSquare bool
	}{
		{name: "thumbnail", maxW: 150, maxH: 150, cropSquare: true},
		{name: "preview", maxW: 800, maxH: 600, cropSquare: false},
		{name: "display", maxW: 1200, maxH: 900, cropSquare: false},
	}

	variantDir := filepath.Join(w.storageRoot, "variants")
	if err := os.MkdirAll(variantDir, 0755); err != nil {
		return fmt.Errorf("failed to create variants directory: %w", err)
	}

	completed := 0
	for _, target := range targets {
		if w.delay > 0 {
			timer := time.NewTimer(w.delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}

		var resizedImg image.Image
		if target.cropSquare {
			resizedImg = cropCenterSquare(srcImg, target.maxW)
		} else {
			resizedImg = resizeFitBounds(srcImg, target.maxW, target.maxH)
		}

		bounds := resizedImg.Bounds()
		width, height := bounds.Dx(), bounds.Dy()

		encoded, err := encodeVariant(resizedImg, format)
		if err != nil {
			return fmt.Errorf("failed to encode variant image: %w", err)
		}

		outFilename, err := generateStoredFilename(format)
		if err != nil {
			return err
		}

		// Write file to disk immediately
		outPath := filepath.Join(variantDir, outFilename)
		if err := os.WriteFile(outPath, encoded, 0o644); err != nil {
			return fmt.Errorf("failed to write variant file: %w", err)
		}

		// Begin transaction for this variant milestone
		tx, err := w.models.DB.BeginTx(ctx, nil)
		if err != nil {
			_ = os.Remove(outPath)
			return fmt.Errorf("failed to start transaction: %w", err)
		}

		variant := &data.Variant{
			ImageID:        job.ImageID,
			Name:           target.name,
			StoredFilename: outFilename,
			Width:          width,
			Height:         height,
			SizeBytes:      int64(len(encoded)),
		}

		if err := w.models.Variants.InsertTx(tx, variant); err != nil {
			tx.Rollback()
			_ = os.Remove(outPath)
			return fmt.Errorf("failed to insert variant record: %w", err)
		}

		completed++
		stageMsg := fmt.Sprintf("generated %s variant", target.name)

		// Update progress and bump version atomically
		newVersion, err := w.models.Jobs.UpdateProgressTx(ctx, tx, job.ID, stageMsg, completed)
		if err != nil {
			tx.Rollback()
			_ = os.Remove(outPath)
			return fmt.Errorf("failed to update job progress: %w", err)
		}
		job.Version = newVersion

		// Commit transaction
		if err := tx.Commit(); err != nil {
			_ = os.Remove(outPath)
			return fmt.Errorf("failed to commit variant transaction: %w", err)
		}

		// Structured Commit Log
		w.logger.Info("job version committed",
			"job_id", job.ID,
			"version", job.Version,
			"status", job.Status,
			"stage", stageMsg,
			"commit_time", time.Now().UTC().Format(time.RFC3339Nano),
		)

		// Notify observers AFTER commit
		if w.notify != nil {
			w.notify(job.ID)
		}
	}

	// Final completion transaction (can share or follow the last milestone)
	tx, err := w.models.DB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to start completion transaction: %w", err)
	}

	newVersion, err := w.models.Jobs.MarkCompletedTx(ctx, tx, job.ID, completed)
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to mark job completed: %w", err)
	}
	job.Version = newVersion

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit completion transaction: %w", err)
	}

	// Structured Commit Log for completion
	w.logger.Info("job version committed",
		"job_id", job.ID,
		"version", job.Version,
		"status", job.Status,
		"stage", "job completed",
		"commit_time", time.Now().UTC().Format(time.RFC3339Nano),
	)

	// Final notification
	if w.notify != nil {
		w.notify(job.ID)
	}

	return nil
}

func encodeVariant(img image.Image, format string) ([]byte, error) {
	var buf bytes.Buffer
	var err error
	if format == "png" {
		err = png.Encode(&buf, img)
	} else {
		err = jpeg.Encode(&buf, img, &jpeg.Options{Quality: 85})
	}
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// cropCenterSquare crops the central square from src and resizes it to
// targetDim x targetDim -- the thumbnail contract's "exact square; crop
// where necessary."
func cropCenterSquare(src image.Image, targetDim int) image.Image {
	bounds := src.Bounds()
	w, h := bounds.Dx(), bounds.Dy()

	minDim := w
	if h < minDim {
		minDim = h
	}

	startX := bounds.Min.X + (w-minDim)/2
	startY := bounds.Min.Y + (h-minDim)/2
	cropRect := image.Rect(startX, startY, startX+minDim, startY+minDim)

	dst := image.NewRGBA(image.Rect(0, 0, targetDim, targetDim))
	draw.BiLinear.Scale(dst, dst.Bounds(), src, cropRect, draw.Over, nil)
	return dst
}

// resizeFitBounds scales src down to fit inside maxW x maxH while
// preserving aspect ratio (IMG-02) -- used for preview and display.
func resizeFitBounds(src image.Image, maxW, maxH int) image.Image {
	bounds := src.Bounds()
	w, h := bounds.Dx(), bounds.Dy()

	if w <= maxW && h <= maxH {
		return src
	}

	ratioW := float64(maxW) / float64(w)
	ratioH := float64(maxH) / float64(h)

	ratio := ratioW
	if ratioH < ratio {
		ratio = ratioH
	}

	newW := int(float64(w) * ratio)
	newH := int(float64(h) * ratio)

	dst := image.NewRGBA(image.Rect(0, 0, newW, newH))
	draw.BiLinear.Scale(dst, dst.Bounds(), src, bounds, draw.Over, nil)
	return dst
}

func generateStoredFilename(format string) (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	ext := ".jpg"
	if format == "png" {
		ext = ".png"
	}
	return hex.EncodeToString(b) + ext, nil
}