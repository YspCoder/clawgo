#!/usr/bin/env bash
set -euo pipefail

target_dir="${1:-$(pwd)}"
skill_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
templates_dir="$skill_dir/templates"
mkdir -p "$target_dir"

copy_template_if_missing() {
  local target_path="$1"
  local template_name="$2"
  if [[ -f "$target_path" ]]; then
    echo "exists: $target_path"
    return
  fi
  cp "$templates_dir/$template_name" "$target_path"
  echo "created: $target_path"
}

copy_template_if_missing "$target_dir/spec.md" "spec.md"
copy_template_if_missing "$target_dir/tasks.md" "tasks.md"
copy_template_if_missing "$target_dir/checklist.md" "checklist.md"
