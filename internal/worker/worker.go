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
	models      data.Models
	storageRoot string
	logger      *slog.Logger
	delay       time.Duration // artificial per-variant delay; 0 in normal operation
	wg          sync.WaitGroup
}

// New constructs a Worker. delay is applied once per variant while a job
// is being generated, purely to make queueing and processing duration
// observable during measurement (Week 4's five-image burst test). It
// must be 0 for any timing meant to reflect real work, and is wired to
// -worker-delay in cmd/api/main.go rather than hardcoded -- the same
// pattern -report-delay used in the earlier synchronous/async lab.
func New(models data.Models, storageRoot string, logger *slog.Logger, delay time.Duration) *Worker {
	return &Worker{
		models:      models,
		storageRoot: storageRoot,
		logger:      logger,
		delay:       delay,
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
			// Shutdown interrupted this job mid-processing -- it was
			// never actually a failure, so it is left in 'processing'
			// rather than marked failed. Recovering a job left
			// mid-processing after a restart is out of scope for
			// Version 1 (see the Requirements spec's Limitations
			// section: worker-crash recovery).
			w.logger.Info("job processing interrupted by shutdown", "job_id", job.ID)
			return
		}
		w.logger.Error("job processing failed", "job_id", job.ID, "error", err)
		_ = w.models.Jobs.MarkFailed(job.ID, "processing failed due to server image transform error")
		return
	}

	if err := w.models.Jobs.MarkCompleted(job.ID); err != nil {
		w.logger.Error("failed to mark job completed", "job_id", job.ID, "error", err)
	} else {
		w.logger.Info("job completed successfully", "job_id", job.ID)
	}
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

	// Phase 1: generate and encode every variant in memory. No file or
	// database write happens until all three of these succeed.
	built := make([]builtVariant, 0, len(targets))
	for _, target := range targets {
		// Artificial per-variant delay (0 by default; see -worker-delay).
		// Waiting on ctx.Done() alongside the timer means a shutdown
		// signal interrupts the wait immediately instead of blocking it
		// out to completion -- the same select{} pattern used for the
		// simulated report delay in the earlier async lab.
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

		built = append(built, builtVariant{
			name:           target.name,
			storedFilename: outFilename,
			bytes:          encoded,
			width:          width,
			height:         height,
		})
	}

	// Phase 2: all three succeeded in memory -- now persist. Files are
	// written first, tracking what has been written so a failure partway
	// through can be cleaned up; then all three metadata rows are
	// inserted in a single transaction so the database side is atomic
	// too (either all three rows exist, or none do).
	variantDir := filepath.Join(w.storageRoot, "variants")
	if err := os.MkdirAll(variantDir, 0755); err != nil {
		return fmt.Errorf("failed to create variants directory: %w", err)
	}

	var written []string
	cleanup := func() {
		for _, p := range written {
			_ = os.Remove(p)
		}
	}

	for _, b := range built {
		outPath := filepath.Join(variantDir, b.storedFilename)
		if err := os.WriteFile(outPath, b.bytes, 0o644); err != nil {
			cleanup()
			return fmt.Errorf("failed to write variant file: %w", err)
		}
		written = append(written, outPath)
	}

	tx, err := w.models.DB.BeginTx(ctx, nil)
	if err != nil {
		cleanup()
		return fmt.Errorf("failed to start variant metadata transaction: %w", err)
	}
	defer tx.Rollback()

	for _, b := range built {
		variant := &data.Variant{
			ImageID:        job.ImageID,
			Name:           b.name,
			StoredFilename: b.storedFilename,
			Width:          b.width,
			Height:         b.height,
			SizeBytes:      int64(len(b.bytes)),
		}
		if err := w.models.Variants.InsertTx(tx, variant); err != nil {
			cleanup()
			return fmt.Errorf("failed to insert variant record: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		cleanup()
		return fmt.Errorf("failed to commit variant metadata: %w", err)
	}

	// IMG-03: only now, with every variant file written and every row
	// committed together, does the caller mark the job completed.
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