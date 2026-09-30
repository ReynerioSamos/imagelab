#!/usr/bin/env bash
# measure_image.sh -- Week 4 measurement script for ImageLab.
#
# Submits one image, records T_ack from the POST itself, then short-polls
# GET /v1/jobs/{id} every ~1s (same cadence as the browser) until the job
# reaches completed or failed, and reports every metric
# Requirements spec asks for: acknowledgement latency, queue wait,
# processing duration, job duration, polling count, and detection delay.
#
# Usage:
#   ./measure_image.sh path/to/image.jpg [label]
#
# Each run appends one row to measurements.csv so multiple runs (or a
# parallel burst) can be compared later.

set -u

api='http://localhost:4000/v1'
image_path="${1:?usage: measure_image.sh <path-to-image> [label]}"
label="${2:-run}"
csv_file='measurements.csv'

if [[ ! -f "$image_path" ]]; then
  echo "error: no such file: $image_path" >&2
  exit 1
fi
command -v jq >/dev/null || { echo "error: jq is required" >&2; exit 1; }

tmp_body=$(mktemp)
trap 'rm -f "$tmp_body"' EXIT

# --- Step 1: submit
#
# The body is written straight to a file with --output, and --write-out
# is used ONLY for the two numeric fields, printed on their own lines to
# stdout. This matters because ImageLab's writeJSON pretty-prints
# responses (json.MarshalIndent), so a response body itself contains
# embedded newlines would make the two impossible to reliably tell apart.
#
# curl's own time_total for THIS request is spec's definition
# of acknowledgement latency: "response received minus POST start time."
read -r http_code ack_time < <(curl --silent --show-error \
  --output "$tmp_body" \
  --write-out '%{http_code} %{time_total}' \
  --request POST \
  --form "image=@${image_path}" \
  "$api/images")

if [[ "$http_code" != "202" ]]; then
  echo "error: expected 202 Accepted, got $http_code" >&2
  cat "$tmp_body" >&2
  exit 1
fi

job_id=$(jq -r '.job_id' "$tmp_body")
image_id=$(jq -r '.image_id' "$tmp_body")
status_url=$(jq -r '.status_url' "$tmp_body")

if [[ -z "$job_id" || "$job_id" == "null" || -z "$status_url" || "$status_url" == "null" ]]; then
  echo "error: 202 response did not include job_id/status_url" >&2
  cat "$tmp_body" >&2
  exit 1
fi

echo "submitted image_id=$image_id job_id=$job_id ack_time=${ack_time}s"

# --- Step 2: short-poll
#
# GET /v1/jobs/{id} wraps the job under a top-level "job" key --
# {"job": {"id": ..., "status": ..., ...}}
polls=0
job_json=""
while true; do
  job=$(curl --silent --show-error "$api${status_url#/v1}")
  status=$(printf '%s' "$job" | jq -r '.job.status')
  polls=$((polls + 1))
  printf 'poll=%d status=%s\n' "$polls" "$status"

  case "$status" in
    completed|failed)
      job_json="$job"
      # detection_epoch is the client's own wall clock at the instant it
      # observed a terminal state -- one side of "detection delay"; the
      # other side (completed_at/failed_at) comes from the server.
      detection_epoch=$(date +%s.%N)
      break
      ;;
  esac
  sleep 1
done

# --- Step 3: pull the timestamps the server actually recorded
queued_at=$(printf '%s' "$job_json" | jq -r '.job.queued_at')
started_at=$(printf '%s' "$job_json" | jq -r '.job.started_at')
terminal_at=$(printf '%s' "$job_json" | jq -r '.job.completed_at // .job.failed_at')
final_status=$(printf '%s' "$job_json" | jq -r '.job.status')

to_epoch() { date -u -d "$1" +%s.%N 2>/dev/null; }

queued_epoch=$(to_epoch "$queued_at")
started_epoch=$(to_epoch "$started_at")
terminal_epoch=$(to_epoch "$terminal_at")

# --- Step 4: compute every required metric
queue_wait=$(awk -v a="$queued_epoch"   -v b="$started_epoch"  'BEGIN{printf "%.3f", b-a}')
processing=$(awk -v a="$started_epoch"  -v b="$terminal_epoch" 'BEGIN{printf "%.3f", b-a}')
job_duration=$(awk -v a="$queued_epoch" -v b="$terminal_epoch" 'BEGIN{printf "%.3f", b-a}')
detection_delay=$(awk -v a="$terminal_epoch" -v b="$detection_epoch" 'BEGIN{printf "%.3f", b-a}')

echo
echo "----- summary ($label) -----"
printf 'image_id=%s\n'         "$image_id"
printf 'job_id=%s\n'           "$job_id"
printf 'final_status=%s\n'     "$final_status"
printf 'ack_time=%ss\n'        "$ack_time"
printf 'queue_wait=%ss\n'      "$queue_wait"
printf 'processing=%ss\n'      "$processing"
printf 'job_duration=%ss\n'    "$job_duration"
printf 'polls=%d\n'            "$polls"
printf 'detection_delay=%ss\n' "$detection_delay"

if [[ "$final_status" == "completed" ]]; then
  echo "variants:"
  printf '%s' "$job_json" | jq -r '.job.variants[] | "  \(.name): \(.width)x\(.height)  \(.url)"'
else
  # Note the server's own key here is "error".
  error_message=$(printf '%s' "$job_json" | jq -r '.job.error // "none"')
  echo "error_message=$error_message"
fi

# --- Step 5: append one CSV row for cross-run comparison
#
# Each invocation appends exactly one line under PIPE_BUF (~4KB), so
# concurrent appends from a parallel burst (see below) do not interleave
# or corrupt each other on Linux -- no locking needed for a file this small.
if [[ ! -f "$csv_file" ]]; then
  echo "label,image_id,job_id,final_status,ack_time,queue_wait,processing,job_duration,polls,detection_delay" > "$csv_file"
fi
printf '%s,%s,%s,%s,%s,%s,%s,%s,%d,%s\n' \
  "$label" "$image_id" "$job_id" "$final_status" \
  "$ack_time" "$queue_wait" "$processing" "$job_duration" "$polls" "$detection_delay" \
  >> "$csv_file"

echo
echo "appended row to $csv_file"

# --- Five-image burst
# No second script is needed: this script already does exactly "submit
# one image, measure it end to end" -- the burst scenario is just five
# independent invocations started close together. Run with -worker-delay
# set (in main.go) so queue wait is actually observable. Run the code below
# in the terminal
#
#   for i in 1 2 3 4 5; do
#     ./measure_image.sh sample.jpg "burst-$i" &
#   done
#   wait
#
# Each instance submits its own image, gets its own job_id, and polls
# independently -- but all five POSTs land close together while only one
# worker is claiming from the queue, so job 5's queue_wait should be
# visibly larger than job 1's. Compare the queue_wait column across the
# five "burst-*" rows in measurements.csv afterward.