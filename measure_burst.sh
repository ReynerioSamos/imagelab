# measure_burst.sh -- Week 4 five-image burst measurement for ImageLab.
#
# Fires several copies of the same image at the API close together while
# only one worker drains the queue. This is
# the "submit five images close together and observe queue position with
# one worker" requirement.
#
# measure_image.sh already measures one submission end to end; this
# script's only job is to launch several of them in parallel, at the same
# time, and then summarize the comparison -- it does not duplicate any of
# measure_image.sh's own measurement logic.
#
# Usage:
#   ./measure_burst.sh path/to/image.jpg [count] [label-prefix]
#
# Defaults to 5 copies, labelled "burst". Each copy gets its own job_id
# and polls independently; every row lands in measurements.csv exactly
# like any other measure_image.sh run, tagged burst-1..burst-N.

set -u

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" &> /dev/null && pwd)"
measure_script="$script_dir/measure_image.sh"

image_path="${1:?usage: measure_burst.sh <path-to-image> [count] [label-prefix]}"
count="${2:-5}"
prefix="${3:-burst}"
csv_file='measurements.csv'

if [[ ! -f "$image_path" ]]; then
  echo "error: no such file: $image_path" >&2
  exit 1
fi
if [[ ! -f "$measure_script" ]]; then
  echo "error: measure_image.sh not found next to this script ($measure_script)" >&2
  exit 1
fi
if [[ ! -x "$measure_script" ]]; then
  chmod +x "$measure_script"
fi

echo "submitting $count copies of $image_path close together..."
echo

# All $count invocations are started in this loop before anything waits,
# so the POSTs actually land close together -- staggering them here would
# defeat the point of the scenario.
pids=()
for ((i = 1; i <= count; i++)); do
  "$measure_script" "$image_path" "${prefix}-${i}" &
  pids+=("$!")
done

status=0
for pid in "${pids[@]}"; do
  wait "$pid" || status=1
done

echo
echo "----- burst summary (${prefix}-1 .. ${prefix}-${count}) -----"

if [[ ! -f "$csv_file" ]]; then
  echo "error: $csv_file was not created -- check the output above for a failed run" >&2
  exit 1
fi

# For each label, take the LAST matching row in the CSV rather than every
# match: if this script (or measure_image.sh) has been run before with
# the same prefix, older rows from a previous burst are still in the file
# and must not be mixed into this run's comparison.
rows=""
for ((i = 1; i <= count; i++)); do
  row=$(awk -F',' -v want="${prefix}-${i}" \
    '$1 == want {print $1","$4","$5","$6","$7","$8","$9","$10}' "$csv_file" | tail -n 1)
  rows+="${row}"$'\n'
done

{
  echo "label,final_status,ack_time,queue_wait,processing,job_duration,polls,detection_delay"
  printf '%s' "$rows"
} | { command -v column >/dev/null 2>&1 && column -t -s ',' || cat; }

exit "$status"