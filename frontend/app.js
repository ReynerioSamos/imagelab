// ImageLab frontend
//
// an empty initial state as thats that is needed for week 1
// preview on selection (no network activity yet),
// single-submission upload.
// placeholders for now -- will fill out as weeks progress

const fileInput = document.getElementById("file-input");
const previewArea = document.getElementById("preview-area");
const previewImg = document.getElementById("preview-img");
const metaFilename = document.getElementById("meta-filename");
const metaSize = document.getElementById("meta-size");
const metaType = document.getElementById("meta-type");
const processButton = document.getElementById("process-button");
const uploadStatus = document.getElementById("upload-status");

let selectedFile = null;
let isSubmitting = false; // the state guard, independent of the disabled button

const ACCEPTED_TYPES = ["image/jpeg", "image/png"];
const MAX_BYTES = 10 * 1024 * 1024; // mirrored here only for UX -- the server is authoritative

function formatBytes(bytes) {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(2)} MB`;
}

function resetToInitialState() {
  selectedFile = null;
  fileInput.value = "";
  previewArea.hidden = true;
  if (previewImg.src) URL.revokeObjectURL(previewImg.src);
  previewImg.src = "";
  processButton.disabled = true; // UI-02
  processButton.textContent = "Process image";
  uploadStatus.textContent = "";
}

// selecting a file only ever builds a browser-owned
// preview via an object URL. Nothing in this handler touches the
// network, creates a server resource, or starts polling.
fileInput.addEventListener("change", () => {
  const file = fileInput.files[0];
  uploadStatus.textContent = "";

  if (!file) {
    resetToInitialState();
    return;
  }

  if (!ACCEPTED_TYPES.includes(file.type)) {
    uploadStatus.textContent = "Please choose a JPEG or PNG image.";
    resetToInitialState();
    return;
  }

  if (file.size > MAX_BYTES) {
    uploadStatus.textContent = "That file is larger than the 10 MB limit.";
    resetToInitialState();
    return;
  }

  selectedFile = file;

  if (previewImg.src) URL.revokeObjectURL(previewImg.src);
  previewImg.src = URL.createObjectURL(file);
  metaFilename.textContent = file.name;
  metaSize.textContent = formatBytes(file.size);
  metaType.textContent = file.type;
  previewArea.hidden = false;

  processButton.disabled = false;
});

// exactly one in-flight POST per click,
// guarded both by the disabled attribute and by isSubmitting
processButton.addEventListener("click", async () => {
  if (isSubmitting) return;
  if (!selectedFile) return;

  isSubmitting = true;
  processButton.disabled = true;
  processButton.textContent = "Uploading...";
  uploadStatus.textContent = "";

  try {
    const formData = new FormData();
    formData.append("image", selectedFile);

    const response = await fetch("/v1/images", {
      method: "POST",
      body: formData,
    });

    if (!response.ok) {
      const body = await response.json().catch(() => ({}));
      throw new Error(body.error || `Upload failed (status ${response.status})`);
    }

    const result = await response.json();

    // the server does not yet return a job, so there is
    // nothing to poll. This message is a temporary stand-in
    // later will replace it with the real 202 + job card + polling flow.
    uploadStatus.textContent =
      `Original stored as image #${result.image_id}. ` +
      "Job creation and processing arrive in Week 2.";

    processButton.textContent = "Process image";
  } catch (err) {
    // on failure, restore the ability to submit again.
    uploadStatus.textContent = err.message || "Upload failed. Please try again.";
    processButton.textContent = "Process image";
  } finally {
    isSubmitting = false;
    processButton.disabled = !selectedFile;
  }
});

resetToInitialState();
