// ImageLab frontend -- Week 1 scope.
//
// Implements UI-01..UI-09: an empty initial state, a browser-owned local
// preview on selection (no network activity), and a guarded single
// submission. The job timeline, polling indicator and results grid exist
// in the markup but stay inert until Week 2 (job creation) and Week 3
// (one-second short polling) are built.

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
};

let selectedFile = null;
let isSubmitting = false; // SUB-02: guard held independently of the button's disabled attribute
let previewUrl = null;

// Client-side limits are for fast feedback only. The server re-checks all
// of this (VAL-02) and is the authority -- these values get replaced by
// whatever GET /v1/upload-constraints reports.
let constraints = {
  acceptedMediaTypes: ["image/jpeg", "image/png"],
  acceptedExtensions: [".jpg", ".jpeg", ".png"],
  maxBytes: 10 * 1024 * 1024,
};

/* ---------------- helpers ---------------- */

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

// All user-facing text is set with textContent, never innerHTML, so a
// hostile filename cannot inject markup (Section 13, Minimum Safeguards).
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
  const exts = constraints.acceptedExtensions
    .map((e) => e.replace(".", "").toUpperCase());
  const unique = [...new Set(exts)].join(" or ");
  els.constraintsNote.textContent =
    `${unique} \u00b7 up to ${formatBytes(constraints.maxBytes)}`;
}

/* -------- constraints come from the server -------- */

// Fetching the rules rather than hardcoding them keeps the disclaimer in
// the UI and the validation in the handler from ever disagreeing.
async function loadConstraints() {
  try {
    const res = await fetch("/v1/upload-constraints");
    if (!res.ok) return; // keep the built-in defaults
    const body = await res.json();
    constraints = {
      acceptedMediaTypes: body.accepted_media_types ?? constraints.acceptedMediaTypes,
      acceptedExtensions: body.accepted_extensions ?? constraints.acceptedExtensions,
      maxBytes: body.max_bytes ?? constraints.maxBytes,
    };
    els.fileInput.setAttribute("accept", constraints.acceptedMediaTypes.join(","));
    renderConstraintsNote();
  } catch {
    // Offline or server not ready -- the defaults above still describe
    // the contract correctly, so there is nothing to surface to the user.
  }
}

/* ---------------- state transitions ---------------- */

function resetToInitialState() {
  selectedFile = null;
  els.fileInput.value = "";

  if (previewUrl) {
    URL.revokeObjectURL(previewUrl); // release the blob; it is not garbage collected on its own
    previewUrl = null;
  }
  els.previewImg.removeAttribute("src");

  els.dropzoneSelected.hidden = true;
  els.dropzoneEmpty.hidden = false;

  els.processButton.disabled = true; // UI-02
  els.processLabel.textContent = "Process image";
}

function showSelected(file) {
  selectedFile = file;

  if (previewUrl) URL.revokeObjectURL(previewUrl);
  previewUrl = URL.createObjectURL(file); // UI-05: preview is browser-owned, nothing is uploaded
  els.previewImg.src = previewUrl;

  els.metaFilename.textContent = file.name;

  // Dimensions are read from the decoded preview, so the line fills in
  // once the browser has the image -- it never requires the server.
  els.metaLine.textContent = `${formatBytes(file.size)} \u00b7 ${shortTypeLabel(file.type)}`;
  els.previewImg.onload = () => {
    const w = els.previewImg.naturalWidth;
    const h = els.previewImg.naturalHeight;
    if (w && h) {
      els.metaLine.textContent =
        `${formatBytes(file.size)} \u00b7 ${shortTypeLabel(file.type)} \u00b7 ${w} \u00d7 ${h}`;
    }
  };

  els.dropzoneEmpty.hidden = true;
  els.dropzoneSelected.hidden = false;

  els.processButton.disabled = false;
}

/* ---------------- selection ---------------- */

// UI-06: selecting a file only builds a local preview. No request is
// sent, no job is created, and no polling starts.
els.fileInput.addEventListener("change", () => {
  const file = els.fileInput.files[0];
  clearMessage();

  if (!file) {
    resetToInitialState();
    return;
  }

  // These two checks are a usability courtesy only -- a user can bypass
  // them entirely (curl, devtools, a renamed file), which is exactly why
  // the server decodes the bytes itself before accepting anything.
  if (!constraints.acceptedMediaTypes.includes(file.type)) {
    resetToInitialState();
    showMessage(
      `"${file.name}" is not a supported image. Choose a JPEG or PNG file.`,
      "error"
    );
    return;
  }

  if (file.size > constraints.maxBytes) {
    resetToInitialState();
    showMessage(
      `"${file.name}" is ${formatBytes(file.size)}, which is over the ` +
      `${formatBytes(constraints.maxBytes)} limit.`,
      "error"
    );
    return;
  }

  showSelected(file); // UI-07: choosing again simply replaces the selection
});

/* ---------------- submission ---------------- */

// UI-08 / UI-09 / SUB-01 / SUB-02: exactly one POST may be in flight.
// The button is disabled synchronously *before* awaiting fetch, and
// isSubmitting guards the handler itself, so neither a rapid double-click
// nor a re-entrant call can start a second upload.
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

    // The server's rejection message is authoritative and more specific
    // than anything the browser checked (e.g. a GIF renamed to .png), so
    // it is surfaced directly rather than replaced with a generic string.
    if (!response.ok) {
      let detail = `Upload failed (HTTP ${response.status}).`;
      try {
        const body = await response.json();
        if (typeof body.error === "string") detail = body.error;
      } catch {
        /* non-JSON body: keep the status-based message */
      }
      throw new Error(detail);
    }

    const result = await response.json();

    // Week 1 stops here: the response is 201 Created with an image id and
    // no job, so there is nothing to poll yet. Week 2 swaps this for the
    // 202 Accepted flow that populates the job card and starts polling.
    showMessage(
      `Original stored. Image ID ${result.image_id}. ` +
      `Job creation and background processing arrive in Week 2.`,
      "success"
    );
  } catch (err) {
    // SUB-03: a rejected upload must leave the user able to try again.
    showMessage(err.message || "Upload failed. Please try again.", "error");
  } finally {
    isSubmitting = false;
    els.processLabel.textContent = "Process image";
    els.processButton.disabled = !selectedFile;
  }
});

/* ---------------- init ---------------- */

resetToInitialState(); // UI-01, UI-03, UI-04
renderConstraintsNote();
loadConstraints();