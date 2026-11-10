#!/bin/bash
# AI picture + product facts: upload, bad pictures, attach to a request.
# Needs: bash, curl, python3. Test data only (it creates a product, a campaign and a lead each run).
#   SMOKE_ADMIN_PASSWORD=... [SMOKE_ADMIN_EMAIL=...] [SMOKE_API=http://localhost:8080] ./scripts/ai-image-smoke-test.sh
# For the staff thread test the API must run with PUBLIC_CHAT_ENABLED=1.
B="${SMOKE_API:-http://localhost:8080}"
j() { python3 -c "import sys,json; d=json.load(sys.stdin); print(eval(sys.argv[1]))" "$1"; }
pass=0; fail=0
chk() { if [ "$2" = "1" ]; then echo "PASS  $1"; pass=$((pass+1)); else echo "FAIL  $1  ($3)"; fail=$((fail+1)); fi; }
TOK=$(curl -s -XPOST $B/auth/login -H 'content-type: application/json' -d "{\"email\":\"${SMOKE_ADMIN_EMAIL:-admin@example.com}\",\"password\":\"${SMOKE_ADMIN_PASSWORD:?set SMOKE_ADMIN_PASSWORD}\"}" | j "d['token']")
A="authorization: Bearer $TOK"; J='content-type: application/json'
PNG=$(python3 -c "
import base64,zlib,struct
def ch(t,d): c=struct.pack('>I',len(d))+t+d; return c+struct.pack('>I',zlib.crc32(t+d)&0xffffffff)
print(base64.b64encode(b'\x89PNG\r\n\x1a\n'+ch(b'IHDR',struct.pack('>IIBBBBB',1,1,8,2,0,0,0))+ch(b'IDAT',zlib.compress(b'\x00\xff\x00\x00'))+ch(b'IEND',b'')).decode())")
P=$(curl -s -XPOST $B/products -H "$A" -H "$J" -d '{"name":"หูฟัง Aria","description":"หูฟังไร้สาย","price":2500,"brand_info":"x"}' | j "d['id']")
curl -s -XPUT $B/product-facts/$P -H "$A" -H "$J" -d '{"variants":"64GB ดำ","product_type":"","after_sales":"","payment_methods":"โอน","installment_info":""}' >/dev/null
C=$(curl -s -XPOST $B/campaigns -H "$A" -H "$J" -d "{\"name\":\"img\",\"product_id\":$P,\"budget\":1000}" | j "d['id']")
R=$(curl -s -XPOST $B/products/$P/image -H "$A" -H "$J" -d "{\"data_url\":\"data:image/png;base64,$PNG\"}"); echo "upload: $R"
R=$(curl -s -XPOST $B/campaigns/$C/plan -H "$A" -H "$J" -d '{}')
chk "plan works with a saved picture; template run does not claim the picture was used" "$(echo "$R" | j "1 if d['generated_by']=='template' and d['image_used']=='' else 0")" "$R"
R=$(curl -s -XPOST $B/campaigns/$C/plan -H "$A" -H "$J" -d "{\"image\":\"data:image/png;base64,$PNG\"}"); chk "plan with a picture attached to the request" "$(echo "$R" | j "1 if 'plan' in d else 0")" "$R"
CODE=$(curl -s -o /dev/null -w '%{http_code}' -XPOST $B/campaigns/$C/plan -H "$A" -H "$J" -d '{"image":"not-an-image"}'); chk "bad attached picture -> 400" "$([ "$CODE" = 400 ] && echo 1 || echo 0)" "$CODE"
HTML=$(echo -n '<html>x</html>' | base64 -w0)
CODE=$(curl -s -o /dev/null -w '%{http_code}' -XPOST $B/campaigns/$C/generate-contents -H "$A" -H "$J" -d "{\"channel\":\"line\",\"count\":2,\"image\":\"data:image/png;base64,$HTML\"}"); chk "HTML disguised as a picture -> 400" "$([ "$CODE" = 400 ] && echo 1 || echo 0)" "$CODE"
R=$(curl -s -XPOST $B/campaigns/$C/generate-contents -H "$A" -H "$J" -d '{}'); chk "generate-contents with no body still works (3 drafts)" "$(echo "$R" | j "1 if len(d['contents'])==3 else 0")" "$R"
echo "== $pass passed, $fail failed"
[ "$fail" = 0 ]
