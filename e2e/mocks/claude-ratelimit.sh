#!/usr/bin/env bash
# Mock claude that emits a stream-json rate-limit rejection and exits 1.
# Used by e2e rate-limit tests — faster and free vs hitting a real quota.
#
# Event sequence:
#   - system:init
#   - stream_event:message_start (minimal usage — cost monitor won't fire)
#   - rate_limit_event status:rejected with resetsAt = now + 3600s
#     (rate_limit_info at top level, matching production claude wire format)
# Then exit 1.
RESETS_AT=$(( $(date +%s) + 3600 ))
printf '{"type":"system","subtype":"init","session_id":"mock-sess-1","cwd":"/mock","tools":[],"model":"claude-haiku-4-5-20251001","permissionMode":"default","apiKeySource":"none"}\n'
printf '{"type":"stream_event","event":{"type":"message_start","message":{"model":"claude-haiku-4-5-20251001","id":"msg_mock","type":"message","role":"assistant","content":[],"stop_reason":null,"usage":{"input_tokens":10,"output_tokens":0,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}},"session_id":"mock-sess-1"}\n'
printf '{"type":"rate_limit_event","rate_limit_info":{"status":"rejected","resetsAt":%d,"rateLimitType":"five_hour","overageStatus":"rejected","overageDisabledReason":"org_level_disabled","isUsingOverage":false},"uuid":"mock-uuid-1","session_id":"mock-sess-1"}\n' "$RESETS_AT"
exit 1
