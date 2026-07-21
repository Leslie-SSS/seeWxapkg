#!/usr/bin/env bash
set -euo pipefail
umask 077

if [[ $# -lt 1 ]]; then
  echo "usage: $0 <wxapkg-path> [base-url] [expected-status]"
  exit 1
fi

WXAPKG_PATH="$1"
BASE_URL="${2:-http://127.0.0.1:9090}"
EXPECTED_STATUS="${3:-}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
VERIFIER_SCRIPT="${SCRIPT_DIR}/../backend/internal/beautify/runtime/verify_artifacts.js"

if [[ -n "${SEEWXAPKG_APP_ID:-}" ]]; then
  APPID="$SEEWXAPKG_APP_ID"
  unset SEEWXAPKG_APP_ID
elif [[ -t 0 ]]; then
  read -r -s -p "AppID（普通包可留空）: " APPID
  echo
else
  IFS= read -r APPID || APPID=""
fi
if [[ -n "$APPID" && ! "$APPID" =~ ^wx[a-f0-9]{16}$ ]]; then
  echo "invalid AppID format" >&2
  exit 1
fi

WORK_DIR="$(mktemp -d "${TMPDIR:-/tmp}/seewx-validate.XXXXXX")"
cleanup() {
  if [[ "${KEEP_VALIDATION_ARTIFACTS:-0}" == "1" ]]; then
    echo "private validation artifacts kept at: $WORK_DIR"
  else
    rm -rf -- "$WORK_DIR"
  fi
}
trap cleanup EXIT

if [[ ! -f "$WXAPKG_PATH" ]]; then
  echo "wxapkg file not found: $WXAPKG_PATH"
  exit 1
fi

echo "Submitting task to $BASE_URL ..."
CURL_CONFIG="${WORK_DIR}/upload.curl"
printf 'form-string = "appId=%s"\n' "$APPID" >"$CURL_CONFIG"
RESPONSE="$(curl -fsS --config "$CURL_CONFIG" -X POST \
  -F "file=@${WXAPKG_PATH}" \
  -F "beautify=true" \
  -F "decompile=true" \
  "${BASE_URL}/api/compile")"
rm -f -- "$CURL_CONFIG"
APPID=""

TASK_ID="$(printf '%s' "$RESPONSE" | python3 -c 'import json,sys; print(json.load(sys.stdin)["taskId"])')"
echo "task accepted (${TASK_ID:0:8}…)"

STATUS=""
for _ in $(seq 1 180); do
  DETAIL="$(curl -fsS "${BASE_URL}/api/tasks/${TASK_ID}")"
  STATUS="$(printf '%s' "$DETAIL" | python3 -c 'import json,sys; print(json.load(sys.stdin)["status"])')"
  echo "status=${STATUS}"
  if [[ "$STATUS" == "completed" || "$STATUS" == "partial" || "$STATUS" == "failed" ]]; then
    break
  fi
  sleep 2
done

if [[ "$STATUS" != "completed" && "$STATUS" != "partial" && "$STATUS" != "failed" ]]; then
  echo "task did not reach a terminal state" >&2
  exit 1
fi

REPORT_PATH="${WORK_DIR}/recovery-report.json"
DIAGNOSTICS_PATH="${WORK_DIR}/diagnostics.json"
TASK_PATH="${WORK_DIR}/task.json"
ARTIFACTS_PATH="${WORK_DIR}/artifacts.json"
ZIP_PATH="${WORK_DIR}/result.zip"
NAMED_REPORTS_DIR="${WORK_DIR}/reports"
mkdir -m 700 "$NAMED_REPORTS_DIR"

curl -fsS "${BASE_URL}/api/tasks/${TASK_ID}" -o "$TASK_PATH"
curl -fsS "${BASE_URL}/api/tasks/${TASK_ID}/report" -o "$REPORT_PATH"
curl -fsS "${BASE_URL}/api/tasks/${TASK_ID}/diagnostics" -o "$DIAGNOSTICS_PATH"
curl -fsS "${BASE_URL}/api/tasks/${TASK_ID}/artifacts" -o "$ARTIFACTS_PATH"
for name in manifest-recovery-report js-recovery-report wxml-recovery-report wxss-recovery-report format-report zip-manifest package-profile; do
  curl -fsS "${BASE_URL}/api/tasks/${TASK_ID}/report?name=${name}" -o "${NAMED_REPORTS_DIR}/${name}.json"
done
if [[ "$STATUS" != "failed" ]]; then
  curl -fsS "${BASE_URL}/api/download/${TASK_ID}" -o "$ZIP_PATH"
  unzip -t "$ZIP_PATH" >/dev/null
fi

python3 - "$REPORT_PATH" "$DIAGNOSTICS_PATH" "$ZIP_PATH" "$TASK_PATH" "$ARTIFACTS_PATH" "$NAMED_REPORTS_DIR" "$VERIFIER_SCRIPT" "$EXPECTED_STATUS" <<'PY'
import json
import os
import re
import subprocess
import sys
import tempfile
import zipfile

(
    report_path,
    diagnostics_path,
    zip_path,
    task_path,
    artifacts_path,
    named_reports_dir,
    verifier_script,
    expected_status,
) = sys.argv[1:]
with open(report_path, "r", encoding="utf-8") as f:
    report = json.load(f)
with open(diagnostics_path, "r", encoding="utf-8") as f:
    diagnostics = json.load(f)
with open(task_path, "r", encoding="utf-8") as f:
    task_detail = json.load(f)
with open(artifacts_path, "r", encoding="utf-8") as f:
    public_artifacts = json.load(f)

score = report.get("score") or {}
profile = report.get("profile") or {}
artifacts = report.get("artifacts") or {}

print("\n=== Manual Validation Summary ===")
print(f"status: {report.get('status')}")
print(f"variant: {profile.get('suspectedVariant')}")
print(f"isEncrypted: {profile.get('isEncrypted')}")
print(f"isWeChat4xLike: {profile.get('isWeChat4xLike')}")
print(f"overall: {score.get('overall')}")
print(f"manifest/js/wxml/wxss: {score.get('manifest')}/{score.get('js')}/{score.get('wxml')}/{score.get('wxss')}")
print(f"verifierPassed: {score.get('verifierPassed')}")
print(f"fallbackUsed: {score.get('fallbackUsed')}")
print(f"generatedRatio: {score.get('generatedRatio')}")
print(f"sourceBreakdown: {artifacts.get('sourceBreakdown')}")
format_stage = next((stage for stage in report.get("stages", []) if stage.get("stage") == "formatting"), None)
if format_stage:
    print(f"formatting: {format_stage.get('metrics')}")
print(f"diagnostics: {len(diagnostics)}")
print(f"zipExists: {os.path.exists(zip_path)} size={os.path.getsize(zip_path) if os.path.exists(zip_path) else 0}")
print("topDiagnostics:")
for item in diagnostics[:10]:
    print(f"  - [{item.get('severity')}] {item.get('code')}: {item.get('message')}")

status = report.get("status")
if status not in {"completed", "partial", "failed"}:
    raise SystemExit(f"invalid report status: {status}")
if expected_status and status != expected_status:
    raise SystemExit(f"unexpected terminal status: {status} != {expected_status}")
if task_detail.get("status") != status:
    raise SystemExit(f"task/report status mismatch: {task_detail.get('status')} != {status}")
if not score.get("verifierPassed"):
    raise SystemExit(f"static artifact verifier did not pass: {score}")
if score.get("manifest") != 100:
    raise SystemExit(f"manifest score must be 100 for the release sample: {score}")
files = artifacts.get("files") or []
file_count = artifacts.get("fileCount")
source_breakdown = artifacts.get("sourceBreakdown") or {}
if file_count != len(files):
    raise SystemExit(f"artifact count mismatch: fileCount={file_count}, files={len(files)}")
if sum(source_breakdown.values()) != file_count:
    raise SystemExit(
        f"source breakdown mismatch: total={sum(source_breakdown.values())}, fileCount={file_count}"
    )

url_pattern = re.compile(r"\b(?:https?|wss?|ftp)://[^\s\"'<>]+", re.I)
internal_path_pattern = re.compile(
    r"(?i)(?:/(?:app|data|etc|home|mnt|opt|output|root|run|srv|tmp|usr|users|var|workspace)/|[A-Z]:[\\/]|\\\\[^\\\s]+\\[^\\\s]+\\)"
)

def assert_public_json(label, raw):
    text = raw.decode("utf-8") if isinstance(raw, bytes) else raw
    without_urls = url_pattern.sub("", text)
    match = internal_path_pattern.search(without_urls)
    if match:
        raise SystemExit(f"{label} exposes an internal path near {match.group(0)!r}")
    json.loads(text)

for label, path in {
    "task detail": task_path,
    "online recovery report": report_path,
    "diagnostics": diagnostics_path,
    "artifacts": artifacts_path,
}.items():
    with open(path, "rb") as f:
        assert_public_json(label, f.read())
for filename in sorted(os.listdir(named_reports_dir)):
    with open(os.path.join(named_reports_dir, filename), "rb") as f:
        assert_public_json(f"named report {filename}", f.read())

if status != "failed":
    if not os.path.exists(zip_path) or os.path.getsize(zip_path) == 0:
        raise SystemExit("terminal deliverable has no ZIP archive")
    with zipfile.ZipFile(zip_path) as archive:
        names = archive.namelist()
        if len(names) != len(set(names)):
            raise SystemExit("ZIP contains duplicate entries")
        for name in names:
            if "\\" in name or ":" in name or "\x00" in name:
                raise SystemExit(f"cross-platform unsafe ZIP entry: {name}")
            normalized = name.replace("\\", "/")
            if normalized.startswith("/") or ".." in normalized.split("/"):
                raise SystemExit(f"unsafe ZIP entry: {name}")
        if not names or "src/app.json" not in names:
            raise SystemExit("ZIP does not contain the recovered src tree")
        non_src_entries = sorted(name for name in names if not name.startswith("src/"))
        if non_src_entries:
            raise SystemExit(f"ZIP contains non-src entries: {non_src_entries[:10]}")

        with open(os.path.join(named_reports_dir, "zip-manifest.json"), "rb") as f:
            zip_manifest = json.load(f)
        manifest_files = zip_manifest.get("files") or []
        if len(manifest_files) != len(set(manifest_files)) or sorted(manifest_files) != sorted(names):
            raise SystemExit("zip-manifest does not exactly match archive entries")
        if file_count != len(names):
            raise SystemExit(
                f"artifact summary count does not match ZIP: fileCount={file_count}, ZIP={len(names)}"
            )

        wxml_names = [name for name in names if name.startswith("src/") and name.endswith(".wxml")]
        unresolved_markers = 0
        empty_sentinels = 0
        for name in wxml_names:
            content = archive.read(name).decode("utf-8", errors="replace")
            unresolved_markers += content.count("<!-- seewx-recovery: unresolved text omitted -->")
            unresolved_markers += content.count("<!-- seewx-recovery: unresolved attributes omitted -->")
            empty_sentinels += len(re.findall(r"\bEmpty\b", content))
        if unresolved_markers or empty_sentinels:
            raise SystemExit(
                f"WXML quality gate failed: unresolvedMarkers={unresolved_markers}, Empty={empty_sentinels}"
            )

        suspicious_selector = re.compile(r"(?m)^\s*(?:\.\.?/|/)[^\s{]*\.wxss(?:[.#\[])")
        for name in (entry for entry in names if entry.startswith("src/") and entry.endswith(".wxss")):
            content = archive.read(name).decode("utf-8", errors="replace")
            if suspicious_selector.search(content):
                raise SystemExit(f"WXSS contains a path-polluted selector: {name}")

        app_json = json.loads(archive.read("src/app.json"))
        for item in ((app_json.get("tabBar") or {}).get("list") or []):
            page_path = item.get("pagePath", "") if isinstance(item, dict) else ""
            if page_path.startswith("/") or re.search(r"\.(?:html|wxml|js)$", page_path, re.I):
                raise SystemExit(f"tabBar route was not normalized: {page_path!r}")

        with tempfile.TemporaryDirectory(prefix="seewx-verify-") as extract_dir:
            archive.extractall(extract_dir)
            completed = subprocess.run(
                ["node", verifier_script, os.path.join(extract_dir, "src")],
                check=True,
                capture_output=True,
                text=True,
            )
            parser_result = json.loads(completed.stdout)
            parser_errors = (
                parser_result.get("jsErrors", [])
                + parser_result.get("wxmlErrors", [])
                + parser_result.get("wxssErrors", [])
                + parser_result.get("wxmlMissingRefs", [])
            )
            if parser_errors:
                raise SystemExit(f"extracted artifact parser failed: {parser_errors[:5]}")

    online_packaging = report.get("packaging") or {}
    if not online_packaging.get("downloadReady") or online_packaging.get("archiveSize") != os.path.getsize(zip_path):
        raise SystemExit(f"online archive metadata is inconsistent: {online_packaging}")
    if report.get("snapshotScope") != "live-task":
        raise SystemExit(f"online report scope is misleading: {report.get('snapshotScope')}")
    expected_manifest_url = "report?name=zip-manifest"
    if online_packaging.get("zipManifest") != expected_manifest_url:
        raise SystemExit(f"online zip-manifest reference is inconsistent: {online_packaging}")

if format_stage:
    metrics = format_stage.get("metrics") or {}
    if metrics.get("failed", 0) or metrics.get("skipped", 0):
        raise SystemExit(f"formatting acceptance failed: {metrics}")
    if metrics.get("formatted", 0) <= 0:
        raise SystemExit(f"formatting had no measurable effect: {metrics}")

verify_stage = next((stage for stage in report.get("stages", []) if stage.get("stage") == "verifying"), None)
if not verify_stage:
    raise SystemExit("verification stage is missing")
verify_metrics = verify_stage.get("metrics") or {}
for key in ("wxmlPlaceholderCount", "wxmlUnresolvedMarkers", "wxmlSuspiciousEventBindings"):
    if verify_metrics.get(key, 0) != 0:
        raise SystemExit(f"verification metric {key} is not clean: {verify_metrics}")
PY

if [[ "$STATUS" == "failed" ]]; then
  echo "validation failed: task ended in failed status" >&2
  exit 1
fi

echo "validation passed; temporary files will be removed"
