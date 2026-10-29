#!/usr/bin/env bash
# Isolated fleet control-plane tests. Herdr's production guards have Rust coverage.
set -euo pipefail
ROOT=$(cd "$(dirname "$0")/.." && pwd)
T=$(mktemp -d); trap 'rm -rf "$T"' EXIT
export XDG_STATE_HOME="$T/state" HERDR_ENV=1 HERDR_PANE_ID=lead TEST_ROOT="$T"
mkdir -p "$T/bin" "$XDG_STATE_HOME/agent-fleet/runs/top/workers" "$XDG_STATE_HOME/agent-fleet/runs/child/workers"
export PATH="$T/bin:$PATH"
cat > "$T/bin/herdr" <<'PY'
#!/usr/bin/env python3
import json, os, sys
from pathlib import Path
p=Path(os.environ['TEST_ROOT']); args=sys.argv[1:]
with (p/'calls').open('a') as f: f.write(json.dumps(args)+'\n')
if os.environ.get('VERIFY_LOCK') == '1':
 import fcntl
 with (Path(os.environ['XDG_STATE_HOME'])/'agent-fleet/control.lock').open('a') as lock:
  try:
   fcntl.flock(lock,fcntl.LOCK_EX|fcntl.LOCK_NB)
   print(json.dumps({'error':{'code':'launcher_lock_missing'}})); sys.exit(1)
  except BlockingIOError:
   print(json.dumps({'error':{'code':'agent_pane_not_found'}})); sys.exit(1)
if args[:1] != ['subtree']:
 print(json.dumps({'result':{'agents':[], 'deliveries':[]}})); sys.exit(0)
a=args[1]; id=args[args.index('--id')+1] if '--id' in args else 'fleet-top'
state=p/'server.json'
record=json.loads(state.read_text()) if state.exists() else {'id':id,'root':'lead','state':'pausing','members':[{'pane_id':'lead','identity':{'name':'lead'}},{'pane_id':'worker','identity':{'name':'worker'}}], 'links':[{'pane_id':'worker','supervisor_pane_id':'lead','report':[]}], 'jobs':[{'name':'pr-watch-test','command':['/skills/pr-watch.sh','owner/repo','123']}], 'deliveries':[], 'quarantined':[], 'problems':[], 'confirmed':[], 'pending':['lead','worker']}
if a=='pause' and (p/'fail-pause').exists(): sys.exit(1)
if a=='pause' and record['id'] != id: record.update(id=id,state='pausing')
if a=='resume': record['state']='resumed'
if a=='release': record['state']='released'
if (p/'omit-owned').exists():
 record.update(state='paused',members=[{'pane_id':'lead','identity':{'name':'lead'}}],links=[],confirmed=['lead'],pending=[])
elif a=='pause':
 record.update(state='pausing',members=[{'pane_id':'lead','identity':{'name':'lead'}},{'pane_id':'worker','identity':{'name':'worker'}}],links=[{'pane_id':'worker','supervisor_pane_id':'lead','report':[]}],pending=['lead','worker'])
state.write_text(json.dumps(record))
print(json.dumps({'result':{'pause':record}}))
PY
chmod +x "$T/bin/herdr"
F="$ROOT/skill/scripts/fleet.sh"
R="$XDG_STATE_HOME/agent-fleet/runs"
bind_approval() {
 jq --arg id "$(jq -r .id "$R/$2/pause.json")" --arg operation "$3" '. + {pause_id:$id,operation:$operation}' "$1" > "$1.tmp"
 mv "$1.tmp" "$1"
}
printf lead > "$R/top/supervisor"; printf worker > "$R/child/supervisor"
printf '[{"dispatch":"d1","agent":"worker","pane_id":"worker","status":"working"},{"dispatch":"old","agent":"ghost","pane_id":"old-pane","status":"settled","released":"2026-01-01T00:00:00Z"}]' > "$R/top/ledger.json"
mkdir -p "$R/historical/workers"; printf old-pane > "$R/historical/supervisor"
printf '[{"dispatch":"old-child","agent":"ghost-child","pane_id":"old-child-pane","status":"working"}]' > "$R/historical/ledger.json"
printf '{"run":"top","dispatch":"old"}' > "$R/historical/parent"
printf '[]' > "$R/child/ledger.json"
printf '{"run":"top","dispatch":"d1"}' > "$R/child/parent"
printf 'status,worker_done' > "$R/top/wake-types"
printf '{"id":"mail-preserved","type":"status"}\n' > "$R/top/inbox.jsonl"
cp "$R/top/ledger.json" "$T/ledger.before"

"$F" pause --run top > "$T/receipt"
jq -e '.state == "pausing" and (.runs | index("child") != null) and (.runs | index("historical") == null) and (.agents | index("ghost") == null)' "$R/top/pause.json" >/dev/null
cmp "$R/top/ledger.json" "$T/ledger.before"
grep -q mail-preserved "$R/top/inbox.jsonl"
# A root-only server receipt cannot confirm a ledger-owned child whose link is missing.
touch "$T/omit-owned"
"$F" pause-status --run top > "$T/omitted"
jq -e '.state == "failed" and (.verification.missing_members | index("worker") != null)' "$R/top/pause.json" >/dev/null
rm "$T/omit-owned"
"$F" pause --run top > "$T/recovered-coverage"
# An unpaused ancestor cannot send/retry/release a protected descendant through another run.
mkdir -p "$R/outside/workers"
printf outside > "$R/outside/supervisor"
printf '[{"dispatch":"other","agent":"worker","pane_id":"worker","status":"working"}]' > "$R/outside/ledger.json"
printf '{"id":"paused-done","from":"worker","type":"worker_done","outcome":"succeeded"}\n' > "$R/outside/inbox.jsonl"
if HERDR_PANE_ID=outside "$F" send --run outside --agent worker --subject work --body work > "$T/out" 2>&1; then echo 'FAIL: outside send bypassed pause'; exit 1; fi
grep -q 'paused\|pausing' "$T/out"
if HERDR_PANE_ID=outside "$F" start --run outside --name worker > "$T/out" 2>&1; then exit 1; fi
grep -q 'paused\|pausing' "$T/out"
for cmd in retry resume stop release abandon; do
 if HERDR_PANE_ID=outside "$F" "$cmd" --run outside --dispatch other > "$T/out" 2>&1; then echo "FAIL: outside $cmd bypassed pause"; exit 1; fi
 grep -q 'paused\|pausing' "$T/out"
done
if HERDR_PANE_ID=outside "$F" ack --run outside --id paused-done --decision release > "$T/out" 2>&1; then echo 'FAIL: outside ack released paused worker'; exit 1; fi
grep -q 'paused\|pausing' "$T/out"
if HERDR_PANE_ID=outside "$F" start --run outside --name ghost > "$T/out" 2>&1; then exit 1; fi
if grep -q 'subtree paused' "$T/out"; then echo 'FAIL: released historical owner blocked unrelated work'; exit 1; fi
# Ordinary paths must fail before parsing task inputs or touching attempts.
for cmd in start retry resume send reply stop release abandon adopt; do
 if "$F" "$cmd" --run child >"$T/out" 2>&1; then echo "FAIL: $cmd bypassed pause"; exit 1; fi
 grep -q 'paused\|pausing' "$T/out"
done
if "$ROOT/skill/scripts/herdr-fleet.sh" --run-dir "$R/child" --name worker > "$T/out" 2>&1; then echo 'FAIL: direct launcher bypass'; exit 1; fi
grep -q 'paused\|pausing' "$T/out"
# Mail is retained, but cannot nudge a paused supervisor.
"$F" report --run child --from worker --type status --subject PAUSED --body checkpoint > "$T/report"
"$F" pause --run top > "$T/repeated"
"$F" pause-status --run top > "$T/status"
if "$F" unpause --run top > "$T/out" 2>&1; then echo 'FAIL: unauthorised resume'; exit 1; fi
# Reconciliation is an explicit, recorded operator decision, not a timer.
printf '{"authorization":"Matt requested resume","ownership_checked":true,"pr_state_checked":true,"jobs":[]}' > "$T/approval.json"
bind_approval "$T/approval.json" top resume
printf '#!/bin/sh\necho CLOSED\n' > "$T/bin/gh"; chmod +x "$T/bin/gh"
printf '{"authorization":"Matt requested resume","ownership_checked":true,"pr_state_checked":true,"jobs":["pr-watch-test"]}' > "$T/closed-approval.json"
bind_approval "$T/closed-approval.json" top resume
if "$F" unpause --run top --authorization "$T/closed-approval.json" > "$T/out" 2>&1; then echo 'FAIL: closed PR watch restored'; exit 1; fi
grep -q 'not OPEN' "$T/out"
printf '[{"dispatch":"unexpected"}]' > "$R/child/ledger.json"
if "$F" unpause --run top --authorization "$T/approval.json" > "$T/out" 2>&1; then echo 'FAIL: ownership drift accepted'; exit 1; fi
grep -q 'ownership changed' "$T/out"
printf '[]' > "$R/child/ledger.json"
"$F" unpause --run top --authorization "$T/approval.json" > "$T/resume"
jq -e '.state == "resumed"' "$R/top/pause.json" >/dev/null
# Ownership drift can be accepted only as the exact explicitly authorized current ledger.
"$F" pause --run top > "$T/ownership-pause"
if "$F" unpause --run top --authorization "$T/approval.json" > "$T/out" 2>&1; then echo 'FAIL: stale approval resumed a different pause'; exit 1; fi
grep -q 'authorization.*pause' "$T/out"
printf '[{"dispatch":"authorized-replacement"}]' > "$R/child/ledger.json"
printf '{"authorization":"Matt approved the inspected ownership replacement","ownership_checked":true,"pr_state_checked":true,"jobs":[],"ownership":{"child":[{"dispatch":"authorized-replacement"}]}}' > "$T/reconciled-approval.json"
bind_approval "$T/reconciled-approval.json" top resume
"$F" unpause --run top --authorization "$T/reconciled-approval.json" > "$T/ownership-resume"
# A failed server operation leaves the durable intent fencing work after process restart.
touch "$T/fail-pause"
if "$F" pause --run top > "$T/out" 2>&1; then echo 'FAIL: ignored server failure'; exit 1; fi
jq -e '.state == "pausing"' "$R/top/pause.json" >/dev/null
if "$F" start --run child > "$T/out" 2>&1; then exit 1; fi
rm "$T/fail-pause"
"$F" pause --run top > "$T/recover"
cp "$R/top/pause.json" "$T/pause.saved"
printf '{broken' > "$R/top/pause.json"
if "$F" pause --run top > "$T/out" 2>&1; then echo 'FAIL: overwrote corrupt pause'; exit 1; fi
[ "$(cat "$R/top/pause.json")" = '{broken' ] || { echo 'FAIL: changed corrupt record'; exit 1; }
cp "$T/pause.saved" "$R/top/pause.json"
jq '.runs=[]' "$T/pause.saved" > "$R/top/pause.json"
if "$F" pause --run top > "$T/out" 2>&1; then echo 'FAIL: accepted incomplete control scope'; exit 1; fi
jq -e '.runs | length == 0' "$R/top/pause.json" >/dev/null
cp "$T/pause.saved" "$R/top/pause.json"
# Explicit release is a different decision from resume and does not rearm work.
bind_approval "$T/approval.json" top resume
if "$F" pause-release --run top --authorization "$T/approval.json" > "$T/out" 2>&1; then echo 'FAIL: resume approval authorized teardown'; exit 1; fi
bind_approval "$T/approval.json" top release
"$F" pause-release --run top --authorization "$T/approval.json" > "$T/released"
jq -e '.state == "released"' "$R/top/pause.json" >/dev/null
"$F" pause-status --run top > "$T/released-status"
jq -e '.state == "released"' "$R/top/pause.json" >/dev/null
if "$F" start --run child > "$T/out" 2>&1; then exit 1; fi
if grep -q 'subtree paused' "$T/out"; then echo 'FAIL: explicit release did not authorize cleanup'; exit 1; fi
# An unrelated inherited descriptor 8 is not proof of fleet lock ownership.
HERDR_PANE_ID=outside VERIFY_LOCK=1 "$ROOT/skill/scripts/herdr-fleet.sh" --check-session '{"agent":"pi","kind":"path","value":"/not-used"}' 8>"$T/unrelated-fd" > "$T/lock-check"
# A partially started attempt with no pane identity cannot be silently counted as covered.
mkdir -p "$R/uncertain/workers"; printf lead > "$R/uncertain/supervisor"
printf '[{"dispatch":"unknown-pane","status":"failed_start","agent":null,"launch":{"name":"unknown"}}]' > "$R/uncertain/ledger.json"
"$F" pause --run uncertain > "$T/uncertain"
jq -e '.state == "failed" and (.verification.unresolved_attempts | length == 1)' "$R/uncertain/pause.json" >/dev/null
if "$F" unpause --run uncertain --authorization "$T/approval.json" > "$T/out" 2>&1; then echo 'FAIL: unresolved attempt resumed'; exit 1; fi
bind_approval "$T/approval.json" uncertain release
"$F" pause-release --run uncertain --authorization "$T/approval.json" > "$T/uncertain-release"
# No destructive fallback or supervision clear may occur.
if grep -Eq 'cancel-delivery|"--clear"|"remove"|"send-keys"' "$T/calls"; then echo 'FAIL: destructive pause'; exit 1; fi
echo 'PASS: fleet pause lifecycle, nested gates, restart, failure and explicit resume'
