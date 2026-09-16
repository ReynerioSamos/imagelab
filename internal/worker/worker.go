package worker

import (
	"context"
	"crypto/rand"
	"encoding/hex"
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
	wg          sync.WaitGroup
}

func New(models data.Models, storageRoot string, logger *slog.Logger) *Worker {
	return &Worker{
		models:      models,
		storageRoot: storageRoot,
		logger:      logger,
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

	if err := w.executeJob(job); err != nil {
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

func (w *Worker) executeJob(job *data.Job) error {
	imgRecord, err := w.models.Images.Get(job.ImageID)
	if err != nil {
		return fmt.Errorf("failed to get image record: %w", err)
	}

	// Ensure variants directory exists
	variantDir := filepath.Join(w.storageRoot, "variants")
	if err := os.MkdirAll(variantDir, 0755); err != nil {
		return fmt.Errorf("failed to create variants directory: %w", err)
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
		name      string
		maxDim    int
	}{
		{"thumbnail", 150},
		{"preview", 600},
		{"display", 1200},
	}

	for _, target := range targets {
		resizedImg := resizePreserveAspect(srcImg, target.maxDim)
		bounds := resizedImg.Bounds()
		width, height := bounds.Dx(), bounds.Dy()

		outFilename, err := generateStoredFilename(format)
		if err != nil {
			return err
		}

		outPath := filepath.Join(w.storageRoot, "variants", outFilename)
		outFile, err := os.Create(outPath)
		if err != nil {
			return fmt.Errorf("failed to create variant file: %w", err)
		}

		if format == "png" {
			err = png.Encode(outFile, resizedImg)
		} else {
			err = jpeg.Encode(outFile, resizedImg, &jpeg.Options{Quality: 85})
		}
		outFile.Close()
		if err != nil {
			return fmt.Errorf("failed to encode variant image: %w", err)
		}

		fi, err := os.Stat(outPath)
		if err != nil {
			return err
		}

		variant := &data.Variant{
			ImageID:        job.ImageID,
			Name:           target.name,
			StoredFilename: outFilename,
			Width:          width,
			Height:         height,
			SizeBytes:      fi.Size(),
		}

		if err := w.models.Variants.Insert(variant); err != nil {
			return fmt.Errorf("failed to insert variant record: %w", err)
		}
	}

	return nil
}

func resizePreserveAspect(src image.Image, maxDim int) image.Image {
	bounds := src.Bounds()
	w, h := bounds.Dx(), bounds.Dy()

	if w <= maxDim && h <= maxDim {
		return src
	}

	var newW, newH int
	if w > h {
		newW = maxDim
		newH = (h * maxDim) / w
	} else {
		newH = maxDim
		newW = (w * maxDim) / h
	}

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