// app_2.js - Full Week 3 implementation

const els = {
  fileInput:        document.getElementById("file-input"),
  dropzoneEmpty:    document.getElementById("dropzone-empty"),
  dropzoneSelected: document.getElementById("dropzone-selected"),
  previewImg:       document.getElementById("preview-img"),
  metaFilename:     document.getElementById("meta-filename"),
  metaLine:         document.getElementById("meta-line"),
  constraintsNote:  document.getElementById("constraints-note"),
  processButton:    document.getElementById("process-button"),
  processLabel:     document.getElementById("process-button-label"),
  uploadMessage:    document.getElementById("upload-message"),

  // Job & Polling elements
  jobIdle:          document.getElementById("job-idle"),
  jobActive:        document.getElementById("job-active"),
  jobId:            document.getElementById("job-id"),
  statusBadge:      document.getElementById("status-badge"),
  timeline:         document.getElementById("timeline"),
  pollingIndicator: document.getElementById("polling-indicator"),
  retrievalError:   document.getElementById("retrieval-error"),
  tryAgainButton:   document.getElementById("try-again-button"),

  // Results elements
  resultsEmpty:     document.getElementById("results-empty"),
  resultsGrid:      document.getElementById("results-grid"),
};

let selectedFile = null;
let isSubmitting = false;
let previewUrl = null;
let pollTimer = null;
let activeStatusUrl = null;

let constraints = {
  acceptedMediaTypes: ["image/jpeg", "image/png"],
  acceptedExtensions: [".jpg", ".jpeg", ".png"],
  maxBytes: 10 * 1024 * 1024,
};

/* ---------------- Helpers ---------------- */

function formatBytes(bytes) {
  if (bytes >= 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
  if (bytes >= 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${bytes} bytes`;
}

function shortTypeLabel(mime) {
  if (mime === "image/jpeg") return "JPEG";
  if (mime === "image/png") return "PNG";
  return mime || "unknown";
}

function showMessage(text, tone) {
  els.uploadMessage.textContent = text;
  els.uploadMessage.dataset.tone = tone;
  els.uploadMessage.hidden = false;
}

function clearMessage() {
  els.uploadMessage.textContent = "";
  els.uploadMessage.hidden = true;
}

function renderConstraintsNote() {
  const exts = constraints.acceptedExtensions.map((e) => e.replace(".", "").toUpperCase());
  const unique = [...new Set(exts)].join(" or ");
  els.constraintsNote.textContent = `${unique} \u00b7 up to ${formatBytes(constraints.maxBytes)}`;
}

async function loadConstraints() {
  try {
    const res = await fetch("/v1/upload-constraints");
    if (!res.ok) return;
    const body = await res.json();
    constraints = {
      acceptedMediaTypes: body.accepted_media_types ?? constraints.acceptedMediaTypes,
      acceptedExtensions: body.accepted_extensions ?? constraints.acceptedExtensions,
      maxBytes: body.max_bytes ?? constraints.maxBytes,
    };
    els.fileInput.setAttribute("accept", constraints.acceptedMediaTypes.join(","));
    renderConstraintsNote();
  } catch {}
}

/* ---------------- State Reset ---------------- */

function stopPolling() {
  if (pollTimer) {
    clearInterval(pollTimer);
    pollTimer = null;
  }
  if (els.pollingIndicator) els.pollingIndicator.hidden = true;
}

function resetToInitialState() {
  stopPolling();
  selectedFile = null;
  activeStatusUrl = null;
  els.fileInput.value = "";

  if (previewUrl) {
    URL.revokeObjectURL(previewUrl);
    previewUrl = null;
  }
  els.previewImg.removeAttribute("src");

  els.dropzoneSelected.hidden = true;
  els.dropzoneEmpty.hidden = false;

  els.processButton.disabled = true;
  els.processLabel.textContent = "Process image";

  if (els.jobIdle) els.jobIdle.hidden = false;
  if (els.jobActive) els.jobActive.hidden = true;
  if (els.statusBadge) {
    els.statusBadge.className = "badge badge-idle";
    els.statusBadge.textContent = "Idle";
  }

  els.resultsEmpty.hidden = false;
  els.resultsGrid.hidden = true;
  els.resultsGrid.innerHTML = "";
  if (els.retrievalError) els.retrievalError.hidden = true;
}

function showSelected(file) {
  selectedFile = file;
  if (previewUrl) URL.revokeObjectURL(previewUrl);
  previewUrl = URL.createObjectURL(file);
  els.previewImg.src = previewUrl;

  els.metaFilename.textContent = file.name;
  els.metaLine.textContent = `${formatBytes(file.size)} \u00b7 ${shortTypeLabel(file.type)}`;
  els.previewImg.onload = () => {
    const w = els.previewImg.naturalWidth;
    const h = els.previewImg.naturalHeight;
    if (w && h) {
      els.metaLine.textContent = `${formatBytes(file.size)} \u00b7 ${shortTypeLabel(file.type)} \u00b7 ${w} \u00d7 ${h}`;
    }
  };

  els.dropzoneEmpty.hidden = true;
  els.dropzoneSelected.hidden = false;
  els.processButton.disabled = false;
}

/* ---------------- Polling & Variant Rendering ---------------- */

function updateTimeline(job) {
  const steps = els.timeline.querySelectorAll(".tl-step");
  const status = job.status;

  steps.forEach((step) => {
    const name = step.dataset.step;
    if (status === "queued") {
      if (name === "accepted") step.dataset.state = "done";
      else step.dataset.state = "pending";
    } else if (status === "processing") {
      if (name === "accepted" || name === "stored") step.dataset.state = "done";
      else if (name === "generating") step.dataset.state = "active";
      else step.dataset.state = "pending";
    } else if (status === "completed") {
      step.dataset.state = "done";
    } else if (status === "failed") {
      if (name === "complete") step.dataset.state = "pending";
    }
  });
}

function renderVariants(variants) {
  els.resultsGrid.innerHTML = "";
  
  // Sort variants: thumbnail, preview, display
  const order = { thumbnail: 1, preview: 2, display: 3 };
  variants.sort((a, b) => (order[a.name] || 99) - (order[b.name] || 99));

  // CHANGED: Force all generated images to display at the same size as the first one
  // (code disabled below - no size changes applied)
  // const firstWidth = variants[0]?.width;
  // const firstHeight = variants[0]?.height;

  variants.forEach((variant) => {
    const card = document.createElement("div");
    card.className = "variant-card";

    const title = variant.name.charAt(0).toUpperCase() + variant.name.slice(1);
    // CHANGED: Use first variant dimensions for all (disabled)
    const dimsWidth = variant.width; // firstWidth ?? variant.width;
    const dimsHeight = variant.height; // firstHeight ?? variant.height;
    
    card.innerHTML = `
      <div class="variant-media">
        <img src="${variant.url}" alt="${variant.name} variant" loading="lazy" />
      </div>
      <div class="variant-body">
        <div>
          <p class="variant-name">${title}</p>
          <p class="variant-dims">${dimsWidth} \u00d7 ${dimsHeight} px</p>
        </div>
        <a href="${variant.url}" download class="variant-download" title="Download ${title}">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/>
          </svg>
        </a>
      </div>
    `;
    els.resultsGrid.appendChild(card);
  });

  els.resultsEmpty.hidden = true;
  els.resultsGrid.hidden = false;
}

async function pollJobStatus() {
  if (!activeStatusUrl) return;

  try {
    const res = await fetch(activeStatusUrl);
    if (!res.ok) throw new Error("Failed to fetch status");

    const data = await res.json();
    const job = data.job;

    if (els.retrievalError) els.retrievalError.hidden = true;

    // Update badge & timeline
    els.statusBadge.className = `badge badge-${job.status}`;
    els.statusBadge.textContent = job.status.charAt(0).toUpperCase() + job.status.slice(1);
    updateTimeline(job);

    if (job.status === "completed") {
      stopPolling();
      if (job.variants && job.variants.length > 0) {
        renderVariants(job.variants);
      }
      showMessage("Job completed successfully!", "success");
    } else if (job.status === "failed") {
      stopPolling();
      showMessage(`Job failed: ${job.error || "Processing failed."}`, "error");
    }
  } catch (err) {
    if (els.retrievalError) els.retrievalError.hidden = false;
  }
}

function startPolling(statusUrl) {
  stopPolling();
  activeStatusUrl = statusUrl;
  els.pollingIndicator.hidden = false;
  pollJobStatus(); // Immediate first check
  pollTimer = setInterval(pollJobStatus, 1000); // 1-second short polling
}

/* ---------------- Event Listeners ---------------- */

els.fileInput.addEventListener("change", () => {
  const file = els.fileInput.files[0];
  clearMessage();

  if (!file) {
    resetToInitialState();
    return;
  }

  if (!constraints.acceptedMediaTypes.includes(file.type)) {
    resetToInitialState();
    showMessage(`"${file.name}" is not a supported image. Choose a JPEG or PNG file.`, "error");
    return;
  }

  if (file.size > constraints.maxBytes) {
    resetToInitialState();
    showMessage(`"${file.name}" is ${formatBytes(file.size)}, which is over the ${formatBytes(constraints.maxBytes)} limit.`, "error");
    return;
  }

  showSelected(file);
});

els.processButton.addEventListener("click", async () => {
  if (isSubmitting || !selectedFile) return;

  isSubmitting = true;
  els.processButton.disabled = true;
  els.processLabel.textContent = "Uploading\u2026";
  clearMessage();

  try {
    const formData = new FormData();
    formData.append("image", selectedFile);

    const response = await fetch("/v1/images", { method: "POST", body: formData });

    if (!response.ok) {
      let detail = `Upload failed (HTTP ${response.status}).`;
      try {
        const body = await response.json();
        if (typeof body.error === "string") detail = body.error;
      } catch {}
      throw new Error(detail);
    }

    const result = await response.json();

    els.jobIdle.hidden = true;
    els.jobActive.hidden = false;
    els.jobId.textContent = result.job_id;

    // Begin short polling
    startPolling(result.status_url);

  } catch (err) {
    showMessage(err.message || "Upload failed. Please try again.", "error");
  } finally {
    isSubmitting = false;
    els.processLabel.textContent = "Process image";
    els.processButton.disabled = !selectedFile;
  }
});

if (els.tryAgainButton) {
  els.tryAgainButton.addEventListener("click", pollJobStatus);
}

/* ---------------- Init ---------------- */

resetToInitialState();
renderConstraintsNote();
loadConstraints();