#!/bin/bash
# Staff chat: after staff replied, the customer's follow-up questions reach the lead page.
# Needs: bash, curl, python3. Test data only (it creates a product, a campaign and a lead each run).
#   SMOKE_ADMIN_PASSWORD=... [SMOKE_ADMIN_EMAIL=...] [SMOKE_API=http://localhost:8080] ./scripts/staff-thread-smoke-test.sh
# For the staff thread test the API must run with PUBLIC_CHAT_ENABLED=1.
B="${SMOKE_API:-http://localhost:8080}"
j() { python3 -c "import sys,json; d=json.load(sys.stdin); print(eval(sys.argv[1]))" "$1"; }
pass=0; fail=0
chk() { if [ "$2" = "1" ]; then echo "PASS  $1"; pass=$((pass+1)); else echo "FAIL  $1  ($3)"; fail=$((fail+1)); fi; }
TOK=$(curl -s -XPOST $B/auth/login -H 'content-type: application/json' -d "{\"email\":\"${SMOKE_ADMIN_EMAIL:-admin@example.com}\",\"password\":\"${SMOKE_ADMIN_PASSWORD:?set SMOKE_ADMIN_PASSWORD}\"}" | j "d['token']")
A="authorization: Bearer $TOK"; J='content-type: application/json'
P=$(curl -s -XPOST $B/products -H "$A" -H "$J" -d '{"name":"iPhone 12","description":"มือสอง สภาพ 90%","price":12900,"brand_info":"x"}' | j "d['id']")
C=$(curl -s -XPOST $B/campaigns -H "$A" -H "$J" -d "{\"name\":\"e2e\",\"product_id\":$P,\"budget\":1000}" | j "d['id']")
SID="e2e$(date +%s)abcdef1234"
NAME="ทดสอบ$RANDOM$RANDOM" # unique per run, so the lead lookup below never picks an older lead
say() { curl -s -XPOST $B/public/chat/messages -H "$J" -d "{\"session_id\":\"$SID\",\"text\":\"$1\"}"; }
say "สวัสดี" >/dev/null
curl -s -XPOST $B/public/chat/consent -H "$J" -d "{\"session_id\":\"$SID\",\"display_name\":\"$NAME\",\"campaign_id\":$C,\"consent_marketing\":false}" >/dev/null
R=$(say "ราคาเท่าไหร่"); echo "bot: $(echo "$R" | j "d['reply']")"
LID=$(curl -s $B/leads -H "$A" | python3 -c "import sys,json; print([l for l in json.load(sys.stdin) if l['display_name']=='$NAME'][0]['id'])")
R=$(curl -s -XPOST $B/leads/$LID/reply -H "$A" -H "$J" -d '{"body":"สวัสดีค่ะ เจ้าหน้าที่ดูแลนะคะ"}')
chk "staff reply stored" "$(echo "$R" | j "1 if d.get('id') else 0")" "$R"
R=$(say "ส่งฟรีไหมคะ"); echo "customer follow-up 1 -> $R"
R2=$(say "แล้วผ่อนได้ไหม"); echo "customer follow-up 2 -> $R2"
TH=$(curl -s $B/leads/$LID/thread -H "$A"); echo "$TH" | python3 -c "import sys,json; [print('  ',m['dir'],m['body']) for m in json.load(sys.stdin)]"
chk "staff sees both follow-ups after replying, in order" "$(echo "$TH" | j "1 if [m['body'] for m in d]==['สวัสดีค่ะ เจ้าหน้าที่ดูแลนะคะ','ส่งฟรีไหมคะ','แล้วผ่อนได้ไหม'] else 0")" "$TH"
IB=$(curl -s $B/inbox -H "$A"); echo "inbox: $IB"
echo "== $pass passed, $fail failed"
[ "$fail" = 0 ]
