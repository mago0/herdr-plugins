#!/usr/bin/env bash
# File mailbox between dispatched agents and their supervisor: one JSON object per line.
#   mail.sh send  --inbox F --from NAME --type T --subject S [--body TEXT | --body-file F] [--outcome O] [--reply-to ID]
#                 [--key K [--since TS]]
#                 a repeat is not appended and prints {sent: <first id>, duplicate: true, acked: <bool>}: with --key, a message
#                 from NAME with key K that is un-acked or sent at or after TS (any time with no --since);
#                 without --key, an un-acked message from NAME with the same content
#   mail.sh read  --inbox F [--id ID] [--unacked] [--types T,T]   # message(s) as JSON lines
#   mail.sh ack   --inbox F --id ID [--id ID ...]                 # mark handled; acked messages are never replayed
#   mail.sh wait  --inbox F [--types T,T] [--timeout SECONDS]     # block until an un-acked message of a wanted type exists
#                                                                # prints MAIL|<id>|<type>|<from>|<subject> per message; exit 124 on timeout
#   mail.sh watch --inbox F [--interval SECONDS] [--types T,T]   # Monitor loop: prints each un-acked message once per process
#
# Types: status question escalation worker_done reply followup
# Delivery is ack-based: `read --unacked`, `wait`, and `watch` see a message until someone acks it,
# so a message sent while no reader ran, or read by a reader that then died, is still delivered.
set -euo pipefail

CMD=${1:-}; shift || true
INBOX= FROM= TYPE= SUBJECT= BODY= BODY_FILE= OUTCOME= REPLY_TO= KEY= SINCE= TYPES= INTERVAL=5 TIMEOUT=0 UNACKED=0
IDS=()
while [ $# -gt 0 ]; do
  case "$1" in
    --inbox) INBOX=$2; shift 2 ;; --from) FROM=$2; shift 2 ;; --type) TYPE=$2; shift 2 ;;
    --subject) SUBJECT=$2; shift 2 ;; --body) BODY=$2; shift 2 ;; --body-file) BODY_FILE=$2; shift 2 ;;
    --outcome) OUTCOME=$2; shift 2 ;; --reply-to) REPLY_TO=$2; shift 2 ;; --id) IDS+=("$2"); shift 2 ;;
    --key) KEY=$2; shift 2 ;; --since) SINCE=$2; shift 2 ;;
    --types) TYPES=$2; shift 2 ;; --interval) INTERVAL=$2; shift 2 ;; --timeout) TIMEOUT=$2; shift 2 ;;
    --unacked) UNACKED=1; shift ;;
    *) echo "mail.sh: unknown option $1" >&2; exit 2 ;;
  esac
done
[ -n "$INBOX" ] || { echo "mail.sh: --inbox is required" >&2; exit 2; }
ACKED="$INBOX.acked"

# Un-acked messages, optionally filtered to a comma-separated type list, as JSON lines.
unacked() {
  touch "$INBOX" "$ACKED"
  # -n with inputs builds the acked set once, so a lookup per message is a key lookup.
  jq -nc --rawfile acked "$ACKED" --arg types "$TYPES" '
    ($acked | split("\n") | map(select(length > 0) | {key: ., value: true}) | from_entries) as $a
    | ($types | if . == "" then [] else split(",") end) as $t
    | inputs
    | select((.id | type) != "string" or ($a[.id] | not))
    | select(($t | length) == 0 or (.type as $ty | $t | any(. == $ty)))' "$INBOX"
}
mail_line() { jq -r '"MAIL|\(.id)|\(.type)|\(.from)|\(.subject | gsub("[\n|]"; " "))"'; }

case "$CMD" in
  send)
    case "$TYPE" in status|question|escalation|worker_done|reply|followup) ;;
      *) echo "mail.sh: --type must be status|question|escalation|worker_done|reply|followup" >&2; exit 2 ;; esac
    [ -n "$FROM" ] && [ -n "$SUBJECT" ] || { echo "mail.sh: --from and --subject are required" >&2; exit 2; }
    [ "$TYPE" != reply ] || [ -n "$REPLY_TO" ] || { echo "mail.sh: --type reply needs --reply-to" >&2; exit 2; }
    [ -z "$BODY_FILE" ] || BODY=$(cat "$BODY_FILE")
    LINE=$(jq -cn --arg id "msg_$(date +%s%N)_$$" --arg ts "$(date -u +%Y-%m-%dT%H:%M:%SZ)" --arg from "$FROM" \
      --arg type "$TYPE" --arg subject "$SUBJECT" --arg body "$BODY" --arg outcome "$OUTCOME" --arg reply_to "$REPLY_TO" \
      --arg key "$KEY" \
      '{id:$id, ts:$ts, from:$from, type:$type, subject:$subject, body:$body}
       + (if $outcome == "" then {} else {outcome:$outcome} end)
       + (if $reply_to == "" then {} else {reply_to:$reply_to} end)
       + (if $key == "" then {} else {key:$key} end)')
    mkdir -p "$(dirname "$INBOX")"; touch "$INBOX" "$ACKED"
    # Lock: bodies can exceed the size the kernel appends atomically, and the repeat check must see
    # every append and ack before it.
    { flock 9
      DUP=$(jq -nc --rawfile acked "$ACKED" --argjson m "$LINE" --arg since "$SINCE" '
        ($acked | split("\n") | map(select(length > 0) | {key: ., value: true}) | from_entries) as $a
        | first(inputs | select(.from == $m.from)
            | if $m.key != null then select(.key == $m.key and (($a[.id] | not) or .ts >= $since))
              else select(($a[.id] | not) and .type == $m.type and .subject == $m.subject and .body == $m.body
                          and .outcome == $m.outcome and .reply_to == $m.reply_to) end)
          | {sent: .id, duplicate: true, acked: ($a[.id] != null)}' "$INBOX" 2>/dev/null || true)
      if [ -n "$DUP" ]; then printf '%s\n' "$DUP"
      else printf '%s\n' "$LINE" >>"$INBOX"; jq -c '{sent: .id}' <<<"$LINE"; fi
    } 9>"$INBOX.lock"
    ;;
  read)
    touch "$INBOX"
    if [ ${#IDS[@]} -gt 0 ]; then
      for ID in "${IDS[@]}"; do jq -c --arg id "$ID" 'select(.id == $id)' "$INBOX"; done
    elif [ "$UNACKED" = 1 ]; then unacked
    elif [ -n "$TYPES" ]; then jq -c --arg types "$TYPES" '($types | split(",")) as $t | select(.type as $ty | $t | any(. == $ty))' "$INBOX"
    else jq -c . "$INBOX"; fi
    ;;
  ack)
    [ ${#IDS[@]} -gt 0 ] || { echo "mail.sh: ack needs --id" >&2; exit 2; }
    ( flock 9; printf '%s\n' "${IDS[@]}" >>"$ACKED" ) 9>"$INBOX.lock"
    printf '%s\n' "${IDS[@]}" | jq -R . | jq -sc '{acked:.}'
    ;;
  wait)
    touch "$INBOX"
    END=$(( $(date +%s) + TIMEOUT ))
    while true; do
      # Arm the watcher before checking so an append between the check and the wait still wakes us.
      REM=0
      if [ "$TIMEOUT" -gt 0 ]; then REM=$(( END - $(date +%s) )); [ "$REM" -gt 0 ] || exit 124; fi
      CHUNK=30; [ "$REM" -gt 0 ] && [ "$REM" -lt "$CHUNK" ] && CHUNK=$REM
      if command -v inotifywait >/dev/null; then
        inotifywait -qq -t "$CHUNK" -e modify -e close_write "$INBOX" >/dev/null 2>&1 & W=$!
      else
        sleep "$INTERVAL" & W=$!
      fi
      OUT=$(unacked | mail_line)
      if [ -n "$OUT" ]; then kill "$W" 2>/dev/null; wait "$W" 2>/dev/null || true; printf '%s\n' "$OUT"; exit 0; fi
      wait "$W" 2>/dev/null || true
    done
    ;;
  watch)
    touch "$INBOX"
    declare -A SEEN
    while true; do
      while IFS= read -r L; do
        [ -n "$L" ] || continue
        ID=${L#MAIL|}; ID=${ID%%|*}
        [ -n "${SEEN[$ID]:-}" ] && continue
        SEEN[$ID]=1; printf '%s\n' "$L"
      done < <(unacked | mail_line)
      sleep "$INTERVAL"
    done
    ;;
  *) echo "mail.sh: command must be send, read, ack, wait or watch" >&2; exit 2 ;;
esac
