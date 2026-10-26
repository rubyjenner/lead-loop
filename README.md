# LeadLoop

AI Agent for marketing and sales: plan campaigns, generate content, publish after human approval,
nurture and score leads, hand qualified leads to sales, and feed results back into the next plan.

## สถานะงาน (อัปเดตทุกครั้งก่อน commit)

อัปเดตล่าสุด: 26 ต.ค. 2569 (เย็น) · เอกสารดีไซน์ฉบับปรับปรุง: AI_Agent_Draft_Design_v5.pdf

**ทำเสร็จแล้ว** (ทดสอบแล้วบนเครื่อง)
- schema ฐานข้อมูล (`db/init.sql`), docker-compose (PostgreSQL, Redis, API), `go.sum`
- API: `GET /health`, `POST/GET /campaigns`
- Products: `POST/GET /products`, แคมเปญผูกสินค้าได้ด้วย `product_id`
- Content flow: `POST/GET /campaigns/:id/contents`, `POST /contents/:id/submit`, `/approve`, `/reject` (รวมกรณี error 404/409)
- AI: `POST /campaigns/:id/plan`, `POST /campaigns/:id/generate-contents` ลองตามลำดับ Claude → Gemini → อัลกอริทึมกฎ/เทมเพลต (ไม่มีทางล้ม) และบันทึกผู้สร้างใน `generated_by` (`claude` / `gemini` / `template` / `human`)
- JSON ตอบกลับเป็น UTF-8 (ภาษาไทยแสดงถูกต้อง)
- ล็อกอิน (JWT, หมดอายุ 8 ชม.) + แยกสิทธิ์ admin / marketing / sales: ทุก endpoint ยกเว้น `/health` และ `/auth/login` ต้องมี token
- รหัสผ่านเก็บแบบ bcrypt, ขั้นต่ำ 8 ตัวอักษร; ล็อกอินผิดได้ไม่เกิน 5 ครั้ง/15 นาที/IP (เกินแล้วได้ 429)
- ผู้อนุมัติ (`approved_by`) มาจากผู้ที่ล็อกอิน ไม่รับจาก body; พอร์ต DB/Redis เปิดเฉพาะ localhost; error ภายในไม่รั่วไปถึง client
- ทดสอบแล้ว: ไม่มี/token ผิด → 401, sales สร้างแคมเปญ → 403, ล็อกอินผิดเกินลิมิต → 429
- จัดการผู้ใช้: ปิด/เปิดผู้ใช้และเปลี่ยน role (`PATCH /users/:id`), รีเซ็ตรหัสผ่านโดย admin, ห้ามปิด/ลดสิทธิ์ admin คนสุดท้าย; ทุกคำขอตรวจ role/สถานะล่าสุดจากฐานข้อมูล (ปิดผู้ใช้แล้วใช้ token เดิมไม่ได้ทันที)
- ผู้ใช้เปลี่ยนรหัสผ่านเอง `POST /auth/password`; ผู้ใช้ที่ admin สร้าง/รีเซ็ตต้องเปลี่ยนรหัสก่อนใช้งานอื่น
- บันทึกการใช้ AI (`ai_usage`: ใคร เมื่อไร ผู้ให้บริการ จำนวน token ไม่เก็บข้อความ) และโควตารายวัน (ค่าเริ่มต้น 30 ครั้ง/ผู้ใช้/วัน ตั้ง `AI_DAILY_QUOTA_PER_USER`; เพดาน token รวม `AI_DAILY_TOKEN_CAP`) เกินแล้วยังใช้งานได้ แต่ระบบตอบด้วยเทมเพลต (`fallback_reason: quota`) · ดูการใช้งานของตัวเอง `GET /ai-usage/me` · แอดมินดูสรุปรายวัน `GET /ai-usage?days=7`
- คิวลองใหม่ด้วย Redis: ถ้า AI ทุกตัวล่มและต้องใช้เทมเพลต ระบบเข้าคิวลอง AI ใหม่ที่ 1 นาที, 5 นาที, 15 นาที, 1 ชม., 3 ชม. (สูงสุด `AI_RETRY_MAX` ครั้ง) แล้วอัปเกรดร่างเป็นฉบับ AI เฉพาะที่ยังเป็น draft ที่คนยังไม่แตะ ผลตอบกลับมี `retry_queued: true` ถ้าไม่มี Redis ใช้คิวในหน่วยความจำแทน (หายเมื่อรีสตาร์ท)
- ความปลอดภัยเพิ่ม: security headers (helmet), จำกัดขนาด request 1 MB, ลิมิตล็อกอินตามอีเมล (10 ครั้ง/15 นาที) เพิ่มจากตาม IP, CORS ปิดไว้จนกว่าจะตั้ง `CORS_ORIGINS`
- กฎบริษัท: ผู้อนุมัติคอนเทนต์ต้องเป็นคนละคนกับผู้สร้าง (`created_by`) ถ้าอนุมัติของตัวเองจะได้ 403 และบันทึกใน audit log; ก่อนใช้ข้อมูลจริงต้องขออนุมัติตามนโยบาย PDPA จาก Deputy Manager
- audit log (`GET /audit-log` เฉพาะ admin): ล็อกอินสำเร็จ/ล้มเหลว, สร้าง/แก้ผู้ใช้, เปลี่ยน/รีเซ็ตรหัสผ่าน, ส่ง/อนุมัติ/ปฏิเสธคอนเทนต์ (ไม่เก็บรหัสผ่านหรือ token)

- สคริปต์ทดสอบอัตโนมัติ `scripts/smoke-test.ps1` (66 รายการ ผ่านทั้งหมดเมื่อ 26 ต.ค.) ครอบคลุมล็อกอิน สิทธิ์ flow คอนเทนต์ กฎผู้อนุมัติ ปิด/เปิดผู้ใช้ audit log และ AI
- ทดสอบเรียก AI จริงแล้ว: Gemini ฟรี (ข้อมูลจำลองเท่านั้น) ด้วย `GEMINI_MODEL=gemini-3.5-flash-lite` ได้ `generated_by: gemini` และคิว Redis อัปเกรดร่างเทมเพลตเป็นฉบับ AI ได้เมื่อ Gemini กลับมา; ตัวเลข token ของ Gemini นับรวม token ที่ใช้คิด (thinking) เป็น output
- โค้ดอยู่บน GitHub แบบ private (`rubyjenner/lead-loop`, branch `main`); `.env` ไม่ถูกติดตาม
- ตารางประมาณการค่าใช้จ่ายต่อผู้ใช้ต่อเดือน (Excel มีสูตร ปรับจำนวนผู้ใช้ ครั้งต่อวัน และ token ได้) ใช้ราคาจากหน้าทางการ ตรวจ 26 ต.ค. 2569

**กำลังทำ**
- Postman collection และเอกสาร API (26 ต.ค.)

**ขั้นต่อไป** (ตามเอกสารดีไซน์ฉบับปรับปรุง 20 ต.ค.)
1. 26 ต.ค.: Postman collection และเอกสาร API (พร้อมแผนภาพ Mermaid)
2. 27 ต.ค.: API ลีดพร้อมช่อง `consent` และให้คะแนนลีดด้วยกฎ
3. ส่งตารางประมาณการค่าใช้จ่ายให้พี่เลี้ยง (ภายในสัปดาห์นี้) และสรุป PDPA 1 หน้าสำหรับ Deputy Manager ก่อนใช้ข้อมูลจริง
4. LINE: บริษัทไม่มี LINE OA ให้ ต้องสร้างเองสำหรับทดสอบ (ข้อมูลจำลอง ทดสอบกับคนที่ยินยอม) ทำช่องทางจำลอง (mock) ก่อนแล้วค่อยต่อ LINE จริง; webhook ต้องเป็น HTTPS สาธารณะ
5. 29 ต.ค.: เดโมภายใน
6. พ.ย. เป็นต้นไป: Frontend (React), LINE พร้อมขอความยินยอม PDPA, server + โดเมนทดลอง + HTTPS (Caddy), ย้ายลิมิตล็อกอินไป Redis

**ปัญหาที่ค้าง / ข้อจำกัดปัจจุบัน**
- ยังไม่มี API key ของ Claude (บริษัทไม่ได้กำหนดผู้ให้บริการ ต้องสร้างเอง): ตอนนี้ใช้ Gemini ฟรีกับข้อมูลจำลองเท่านั้น เพราะแบบฟรี Google อาจนำข้อความไปปรับปรุงผลิตภัณฑ์ ข้อมูลจริงต้องใช้แบบจ่ายเงินและได้รับอนุมัติตาม PDPA
- ชื่อรุ่น Gemini เปลี่ยนบ่อย: `gemini-2.5-*` ได้ 404 กับ key ใหม่ (26 ต.ค.) ถ้าเจอ 404 ให้ดูรายชื่อรุ่นที่ key เรียกได้ (ดูหัวข้อตรวจปัญหา) แล้วตั้ง `GEMINI_MODEL`; ถ้าได้ 503 ให้ลองรุ่นอื่นหรือรอสักครู่ (ผู้ใช้ยังได้เทมเพลตและงานเข้าคิวลองใหม่)
- ยังไม่มีขั้นตอน PDPA ฝั่งลีด (วางแผนไว้แล้ว ดูขั้นต่อไป)
- ยังไม่มี HTTPS (จะทำพร้อม deploy ด้วย Caddy) ห้ามนำไปใช้จริงบนอินเทอร์เน็ตก่อนมี HTTPS
- ลิมิตล็อกอินเก็บในหน่วยความจำของ API: รีสตาร์ท API แล้วนับใหม่ (`docker compose restart api`) จะย้ายไป Redis ตอน deploy; ตั้ง `PROXY_HEADER` เมื่ออยู่หลัง Caddy เพื่อให้ได้ IP จริง; ที่เก็บ token ฝั่งหน้าเว็บ (cookie httpOnly หรือ localStorage) ตัดสินใจตอนทำ React

## Run

    cp .env.example .env
    # แก้ .env: ตั้ง JWT_SECRET (32 ตัวขึ้นไป), ADMIN_EMAIL, ADMIN_PASSWORD (8 ถึง 72 ตัว)
    docker compose up --build

ใน PowerShell ใช้ `Copy-Item .env.example .env` แทน `cp`
สร้าง JWT_SECRET ใน PowerShell: `-join ((48..57)+(65..90)+(97..122) | Get-Random -Count 48 | % {[char]$_})`
ADMIN_EMAIL / ADMIN_PASSWORD ใช้สร้างแอดมินคนแรกตอนตารางผู้ใช้ยังว่างเท่านั้น (ห้าม commit `.env`)

## Login และสิทธิ์

| Role | ทำอะไรได้ |
|---|---|
| admin | ทุกอย่าง + จัดการผู้ใช้ (`POST/GET /users`, `PATCH /users/:id`, `POST /users/:id/reset-password`) + ดู `GET /audit-log` |
| marketing | สร้าง/แก้แคมเปญ สินค้า คอนเทนต์ เรียก AI ส่งอนุมัติ/อนุมัติ/ปฏิเสธ |
| sales | ดูข้อมูล (GET) เท่านั้น (สิทธิ์ของลีดจะเพิ่มตอนทำ leads) |

PowerShell (ใช้ `Read-Host` เพื่อไม่พิมพ์รหัสผ่านลงในคำสั่ง/ประวัติ):

    $body = @{ email='admin@example.com'; password=(Read-Host 'password') } | ConvertTo-Json
    $r = Invoke-RestMethod -Method Post -Uri http://localhost:8080/auth/login -ContentType 'application/json' -Body $body
    $PSDefaultParameterValues['Invoke-RestMethod:Headers'] = @{ Authorization = "Bearer $($r.token)" }
    Invoke-RestMethod http://localhost:8080/auth/me

สร้างผู้ใช้ใหม่ (admin เท่านั้น):

    Invoke-RestMethod -Method Post -Uri http://localhost:8080/users -ContentType 'application/json' `
      -Body '{"email":"sale1@example.com","password":"ตั้งรหัสผ่านที่นี่","role":"sales"}'

คำสั่งด้านล่างทั้งหมดต้องล็อกอินและตั้ง header ตามด้านบนก่อน (macOS/Linux ใช้ `-H "Authorization: Bearer $TOKEN"`)

## Try the first endpoints

macOS / Linux / Git Bash:

    curl localhost:8080/health
    curl -X POST localhost:8080/campaigns -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
      -d '{"name":"Launch","goal":"50 leads","target_audience":"students","budget":10000}'
    curl -H "Authorization: Bearer $TOKEN" localhost:8080/campaigns

Windows PowerShell (`curl` ใน PowerShell เป็น alias ของ `Invoke-WebRequest` จึงใช้ cmdlet แทน):

    Invoke-RestMethod http://localhost:8080/health

    Invoke-RestMethod -Method Post -Uri http://localhost:8080/campaigns `
      -ContentType 'application/json' `
      -Body '{"name":"Launch","goal":"50 leads","target_audience":"students","budget":10000}'

    Invoke-RestMethod http://localhost:8080/campaigns

ทดสอบ content flow (สมมติแคมเปญ id = 1 ถ้าส่งภาษาไทยจาก PowerShell ให้ใช้ `[System.Text.Encoding]::UTF8.GetBytes(...)` เหมือนตัวอย่าง products):

    Invoke-RestMethod -Method Post -Uri http://localhost:8080/campaigns/1/contents `
      -ContentType 'application/json' -Body '{"channel":"line","body":"โปรโมชันเปิดตัว"}'
    Invoke-RestMethod http://localhost:8080/campaigns/1/contents
    Invoke-RestMethod -Method Post http://localhost:8080/contents/1/submit
    Invoke-RestMethod -Method Post http://localhost:8080/contents/1/approve   # ผู้อนุมัติมาจาก token

ทดสอบ products:

    Invoke-RestMethod -Method Post -Uri http://localhost:8080/products `
      -ContentType 'application/json; charset=utf-8' `
      -Body ([System.Text.Encoding]::UTF8.GetBytes('{"name":"iPhone 12","description":"มือสอง สภาพ 90%","price":12900,"brand_info":"ร้านมือถือ"}'))
    Invoke-RestMethod http://localhost:8080/products
    Invoke-RestMethod -Method Post -Uri http://localhost:8080/campaigns `
      -ContentType 'application/json' -Body '{"name":"Phone sale","product_id":1,"budget":5000}'

ทดสอบ AI (ถ้ายังไม่มี key ผลจะมี `"generated_by": "template"`; ตั้ง `AI_PROVIDERS=template` เพื่อปิด AI ภายนอกทั้งหมด):

    Invoke-RestMethod -Method Post http://localhost:8080/campaigns/1/plan
    Invoke-RestMethod -Method Post -Uri http://localhost:8080/campaigns/1/generate-contents `
      -ContentType 'application/json' -Body '{"channel":"line","count":3}'
    Invoke-RestMethod http://localhost:8080/campaigns/1/contents

ทดสอบระบบทั้งชุดอัตโนมัติ (ล็อกอิน สิทธิ์ เปลี่ยนรหัส ปิดผู้ใช้ audit log และ AI) ต้องรัน `docker compose up -d` ก่อน:

    .\scripts\smoke-test.ps1              # ถามรหัสผ่าน admin
    .\scripts\smoke-test.ps1 -RateLimit   # ทดสอบลิมิตล็อกอินด้วย (IP จะถูกบล็อก 15 นาที ให้ docker compose restart api หลังทดสอบ)

สคริปต์สร้างผู้ใช้ `smoke-*@example.com` แล้วปิดไว้ท้ายการทดสอบ ใช้กับฐานข้อมูลสำหรับพัฒนาเท่านั้น

รันสคริปต์ทดสอบอัตโนมัติ (ต้อง `docker compose up -d` ก่อน; ใช้ฐานข้อมูลสำหรับพัฒนาเท่านั้น):

    Set-ExecutionPolicy -Scope Process -ExecutionPolicy Bypass   # ครั้งเดียวต่อหน้าต่าง PowerShell ถ้าสคริปต์ถูกบล็อก
    .\scripts\smoke-test.ps1

แต่ละรอบล็อกอินผิดโดยตั้งใจ 2 ครั้ง ถ้ารัน 2 รอบภายใน 15 นาทีให้ `docker compose restart api` ก่อน

รัน unit test (ไม่ต้องมี Go ในเครื่อง ใช้ Docker):

    docker run --rm -v "${PWD}/backend:/app" -w /app golang:1.22 sh -c "go mod tidy && go test ./..."

## ตรวจปัญหาเบื้องต้น

    docker compose ps
    docker compose logs api
    # ดูรุ่น Gemini ที่ key เรียกได้ (ไม่แสดงตัว key)
    $k = (Select-String -Path .env -Pattern '^GEMINI_API_KEY=(.*)$').Matches.Groups[1].Value.Trim()
    (Invoke-RestMethod -Headers @{'x-goog-api-key'=$k} https://generativelanguage.googleapis.com/v1beta/models).models.name
    docker compose down -v   # ล้าง volume ฐานข้อมูล เมื่อแก้ db/init.sql (init.sql รันเฉพาะตอนสร้าง volume ครั้งแรก)

## หลักการออกแบบสำคัญ (รายละเอียดในเอกสาร Draft Design หัวข้อ 8 และ 9)

- **AI สำรอง:** ถ้า AI ล่ม ช้า หรือเต็มลิมิต ระบบตกไปใช้ AI ตัวที่สอง แล้วอัลกอริทึมกฎ/เทมเพลต แล้วเข้าคิว Redis ลองใหม่ ทุกผลลัพธ์บันทึก `generated_by` และต้องผ่านการอนุมัติจากคนเหมือนกัน (ทำแล้วครบ รวมคิว Redis)
- **ลิมิตและค่าใช้จ่าย AI:** ตั้งเพดานที่ผู้ให้บริการ, บันทึกการใช้งาน (`ai_usage`), โควตาต่อวัน, จำกัดความยาวคำตอบ, เก็บผลลัพธ์ไว้ไม่เรียกซ้ำโดยไม่จำเป็น, ให้คะแนนลีดด้วยกฎเป็นหลัก
- **PDPA:** ขอความยินยอมก่อนเก็บข้อมูลและก่อนส่งให้ AI, เก็บหลักฐานความยินยอม, แยกความยินยอมด้านการตลาด, มีช่องทางลบ/ส่งออกข้อมูลลีด, ปกปิดข้อมูลก่อนส่งให้ AI ภายนอก, ไม่บันทึกข้อมูลลูกค้าลง log ช่วงพัฒนาใช้ข้อมูลจำลองเท่านั้น (ฝ่ายกฎหมายของบริษัทตรวจก่อนใช้งานจริง)

## Structure

- db/init.sql: schema (products, campaigns, contents, leads, messages, metrics); ตาราง `users` สร้างตอน API เริ่มทำงาน
- backend/: Go (Fiber) API (`main.go` routes, `auth.go` login/roles, `ai.go` AI routes, `ai_jobs.go` ai_usage/โควตา/ตัวอัปเกรดงานคิว, `providers.go` Claude/Gemini, `fallback.go` เทมเพลตและ prompt, `jobs.go` + `redisq.go` คิวลองใหม่)
- scripts/smoke-test.ps1: ทดสอบอัตโนมัติผ่าน API (PowerShell)
- docker-compose.yml: PostgreSQL, Redis, API

## แนวปฏิบัติเมื่อใช้ AI ช่วยเขียนโค้ด

- อัปเดตส่วน "สถานะงาน" ทุกครั้งก่อน commit เพื่อให้เริ่มงานต่อได้แม้ limit เต็ม
- commit บ่อย แยกตามหน่วยงานเล็กๆ และ commit ก่อนให้ AI แก้ไฟล์ใหญ่
- ตรวจโค้ดที่ AI เขียน (`git diff`) ทุกครั้งก่อนรวมเข้าโปรเจกต์
- ไม่ใส่ข้อมูลลูกค้าจริงหรือ API key ลงในเครื่องมือ AI; เก็บ key ใน `.env` (ถูก ignore โดย git)

## Next

1. Postman collection, API docs and Mermaid diagrams
2. Leads API with a consent field and rule-based lead scoring
3. Chat channel interface with a mock channel, then LINE (own test OA) with PDPA consent
4. React + Vite frontend, then HTTPS deployment (Caddy) with PROXY_HEADER set