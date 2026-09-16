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

	// Define variant processing specifications
	targets := []struct {
		name      string
		maxW      int
		maxH      int
		cropSquare bool
	}{
		{name: "thumbnail", maxW: 150, maxH: 150, cropSquare: true},
		{name: "preview", maxW: 800, maxH: 600, cropSquare: false},
		{name: "display", maxW: 1200, maxH: 900, cropSquare: false},
	}

	for _, target := range targets {
		var resizedImg image.Image
		if target.cropSquare {
			// Center-crop to 150x150 exact square
			resizedImg = cropCenterSquare(srcImg, target.maxW)
		} else {
			// Fit within bounding box preserving aspect ratio (IMG-02, IMG-04)
			resizedImg = resizeFitBounds(srcImg, target.maxW, target.maxH)
		}

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

		// Insert variant record (IMG-03)
		if err := w.models.Variants.Insert(variant); err != nil {
			return fmt.Errorf("failed to insert variant record: %w", err)
		}
	}

	// IMG-03: Completed only after all 3 variants and metadata are successfully written
	return nil
}

// cropCenterSquare crops the central square from src and resizes to targetDim x targetDim
func cropCenterSquare(src image.Image, targetDim int) image.Image {
	bounds := src.Bounds()
	w, h := bounds.Dx(), bounds.Dy()

	minDim := w
	if h < minDim {
		minDim = h
	}

	// Calculate center crop origin
	startX := bounds.Min.X + (w-minDim)/2
	startY := bounds.Min.Y + (h-minDim)/2
	cropRect := image.Rect(startX, startY, startX+minDim, startY+minDim)

	dst := image.NewRGBA(image.Rect(0, 0, targetDim, targetDim))
	draw.BiLinear.Scale(dst, dst.Bounds(), src, cropRect, draw.Over, nil)
	return dst
}

// resizeFitBounds scales down src to fit inside maxW x maxH while preserving aspect ratio (IMG-02)
func resizeFitBounds(src image.Image, maxW, maxH int) image.Image {
	bounds := src.Bounds()
	w, h := bounds.Dx(), bounds.Dy()

	if w <= maxW && h <= maxH {
		return src
	}

	ratioW := float64(maxW) / float64(w)
	ratioH := float64(maxH) / float64(h)

	// Pick smaller ratio to ensure fitting within bounding box
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