# LeadLoop

AI Agent for marketing and sales: plan campaigns, generate content, publish after human approval,
nurture and score leads, hand qualified leads to sales, and feed results back into the next plan.

## สถานะงาน (อัปเดตทุกครั้งก่อน commit)

อัปเดตล่าสุด: 28 ต.ค. 2569 · เอกสารดีไซน์ฉบับปรับปรุง: AI_Agent_Draft_Design_v5.pdf (จะปรับเป็น v6 ใส่ตารางกฎคะแนนฉบับเต็ม)

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

- สคริปต์ทดสอบเดิม `scripts/smoke-test.ps1` (66 รายการ ผ่านทั้งหมดเมื่อ 26 ต.ค.) ครอบคลุมล็อกอิน สิทธิ์ flow คอนเทนต์ กฎผู้อนุมัติ ปิด/เปิดผู้ใช้ audit log และ AI
- Lead API ทดสอบบน Docker จริงแล้ว (28 ต.ค.): `scripts/leads-smoke-test.ps1` ผ่าน 13 รายการ (สร้างลีดต้องมี consent, event/คะแนน, เหตุผลของคะแนน, เพิ่ม/แก้/ลบกฎ, ลองกฎ, ถอน consent, anonymize)
- `scripts/smoke-test.ps1` ผ่าน 66/66 หลังรวมโค้ดลีด (28 ต.ค.) รวมกับ `leads-smoke-test.ps1` 13 รายการ = 79 รายการ
- แก้ AI ถามกลับแทนการเขียนโฆษณาเมื่อแคมเปญไม่มีสินค้า (Gemini เคยตอบเป็นคำขอข้อมูลแล้วถูกเก็บเป็นร่าง): ปรับ system prompt และ prompt สร้างข้อความให้เขียนจากข้อมูลที่มีเสมอ ตรวจกับ Gemini จริงแล้วได้ 2 ร่างครบ
- ข้อมูลเดโม: `scripts/demo-seed.ps1` สร้างสินค้า แคมเปญ และลีดจำลอง Cold/Warm/Hot (คะแนน 10 / 30 / 80, Hot เป็น `qualified`); ขั้นตอนเดโมและคำถามที่คาดไว้อยู่ใน `docs/DEMO_GUIDE.md`
- แก้ปัญหาภาษาไทยเป็น `?` ในฐานข้อมูล: ซ่อมชื่อ/คำอธิบายกฎ 16 ข้อและคำสำคัญ (`event_keywords`) แล้ว ตรวจด้วยค่า UTF-8 hex; สาเหตุคือส่งไฟล์ SQL ผ่าน pipe ของ PowerShell จึงให้ใช้ `docker compose cp` + `psql -f` แทน
- ทดสอบเรียก AI จริงแล้ว: Gemini ฟรี (ข้อมูลจำลองเท่านั้น) ด้วย `GEMINI_MODEL=gemini-3.5-flash-lite` ได้ `generated_by: gemini` และคิว Redis อัปเกรดร่างเทมเพลตเป็นฉบับ AI ได้เมื่อ Gemini กลับมา; ตัวเลข token ของ Gemini นับรวม token ที่ใช้คิด (thinking) เป็น output
- โค้ดอยู่บน GitHub แบบ private (`rubyjenner/lead-loop`, branch `main`); `.env` ไม่ถูกติดตาม
- ตารางประมาณการค่าใช้จ่ายต่อผู้ใช้ต่อเดือน (Excel มีสูตร ปรับจำนวนผู้ใช้ ครั้งต่อวัน และ token ได้) ใช้ราคาจากหน้าทางการ ตรวจ 26 ต.ค. 2569

**ลีดและกฎคะแนน** (รวมเข้าโปรเจกต์และ build/ทดสอบบน Docker แล้ว 28 ต.ค.)
- `backend/scoring.go`: engine ให้คะแนนลีดด้วยกฎ (เงื่อนไขเป็น JSON ไม่ eval โค้ด) + ตรวจเงื่อนไขก่อนบันทึก + จับ event จากข้อความภาษาไทยด้วยคำสำคัญ
- `backend/scoring_test.go`: unit tests ของ engine **ผ่านทั้งหมด** ด้วย `go test scoring.go scoring_test.go` (ไม่ต่อ DB): ลีดจำลอง 5 แบบตามตารางเกณฑ์, ส่งต่อทันที, ตัดสิทธิ์, เพดานคะแนน, กฎซ้อน, ตรวจเงื่อนไข, คำสำคัญภาษาไทย, งบที่ไม่ทราบต้องไม่ถูกหักคะแนน
- `db/leads.sql`: ตาราง `leads`, `consent_events`, `lead_events`, `scoring_rules`, `scoring_settings`, `event_keywords`, `lead_score_log` + seed กฎและคำสำคัญ
- `backend/leads.go`: endpoint ลีด/consent/event/ข้อความ/สถานะ/export/anonymize และ CRUD กฎ/ทดสอบกฎ/คำนวณใหม่/ตั้งค่า/คำสำคัญ; ลงทะเบียนใน `main.go` หลัง `registerAuthRoutes` แล้ว; ป้องกันการปิด/ลบกฎหลัก และล้าง `name`/`contact` legacy fields ตอน anonymize. build และทดสอบกับ Fiber/pgx/PostgreSQL จริงผ่านแล้ว (smoke test 13 รายการ)

**การรวมเข้าโปรเจกต์**
- `main.go` เรียก `registerLeadRoutes(app, pool)` หลัง Authentication; CORS อนุญาต `GET,POST,PATCH,PUT,DELETE,OPTIONS` เมื่อเปิดใช้
- `db/init.sql` รวม schema ลีดสำหรับฐานข้อมูลใหม่ และ `db/leads.sql` มี migration สำหรับฐานข้อมูลเดิมที่มี `leads` แบบเก่า (`name/contact/consent`)
- เพิ่ม `postman/LeadLoop-Leads.postman_collection.json` และ `docs/LEADS_API.md` พร้อม Mermaid
- ตรวจ `gofmt`, JSON ของ Postman และ unit test ของ engine แล้ว; ยังไม่ได้ import Postman collection ทดลองครบทุก endpoint

**Logic ที่ยังไม่ได้ทำ** (ประมาณ 28 ต.ค.: backend ~60–65%, ทั้งโปรเจกต์ ~40%)
- ช่องทางแชต: รับข้อความลูกค้าเข้าระบบ (mock ก่อน แล้ว LINE webhook)
- AI ตอบลูกค้าตามข้อมูลสินค้า
- เผยแพร่คอนเทนต์ที่อนุมัติแล้วออกช่องทางจริง
- job รายวันบน Redis (`no_reply_3d`, `inactive_7d`) และ nurturing ตามระดับลีดเฉพาะผู้ที่ให้ consent การตลาด
- แจ้งทีมขายเมื่อลีด qualified และมอบหมายลีดให้ sales
- รายงานสรุปลีดตามช่องทางและป้อนผลกลับเข้าแผนรอบถัดไป
- ปกปิดข้อมูลส่วนตัวก่อนส่งให้ AI ภายนอก และกำหนดระยะเก็บข้อมูล
- ย้ายลิมิตล็อกอินไป Redis

**ขั้นต่อไป** (ตามเอกสารดีไซน์ฉบับปรับปรุง)
1. 30 ต.ค.: เดโมและอัปเดตความคืบหน้า (ใช้ `docs/DEMO_GUIDE.md`); import Postman collection ทดลองทุก endpoint
2. ส่งเกณฑ์คะแนนฉบับร่างให้พี่เลี้ยงตรวจ (เกณฑ์ส่งต่อ 50, ความถี่ติดตาม) และตอบคำถามพี่แล้วว่า "ลีด" คือการจัดอันดับลูกค้าแต่ละคน ส่วนการเทียบช่องทางไปอยู่ในส่วนวัดผล (ขั้นที่ 8) ซึ่งต้องเพิ่มรายงานสรุปลีดตามช่องทาง
3. เดโมภายในและอัปเดตความคืบหน้า (ศุกร์ 30 ต.ค.)
4. ส่งตารางประมาณการค่าใช้จ่ายให้พี่เลี้ยง (ส่งแล้ว) และสรุป PDPA 1 หน้าสำหรับ Deputy Manager ก่อนใช้ข้อมูลจริง
5. LINE: บริษัทไม่มี LINE OA ให้ ต้องสร้างเองสำหรับทดสอบ (ข้อมูลจำลอง ทดสอบกับคนที่ยินยอม) ทำช่องทางจำลอง (mock) ก่อนแล้วค่อยต่อ LINE จริง; webhook ต้องเป็น HTTPS สาธารณะ
6. job รายวันบน Redis สร้าง event `no_reply_3d` / `inactive_7d` และ nurturing ตามความถี่ของระดับลีด
7. พ.ย. เป็นต้นไป: Frontend (React) รวมหน้าจัดการกฎคะแนน, LINE พร้อมขอความยินยอม PDPA, server + โดเมนทดลอง + HTTPS (Caddy), ย้ายลิมิตล็อกอินไป Redis

**ปัญหาที่ค้าง / ข้อจำกัดปัจจุบัน**
- ยังไม่มี API key ของ Claude (บริษัทไม่ได้กำหนดผู้ให้บริการ ต้องสร้างเอง): ตอนนี้ใช้ Gemini ฟรีกับข้อมูลจำลองเท่านั้น เพราะแบบฟรี Google อาจนำข้อความไปปรับปรุงผลิตภัณฑ์ ข้อมูลจริงต้องใช้แบบจ่ายเงินและได้รับอนุมัติตาม PDPA
- ชื่อรุ่น Gemini เปลี่ยนบ่อย: `gemini-2.5-*` ได้ 404 กับ key ใหม่ (26 ต.ค.) ถ้าเจอ 404 ให้ดูรายชื่อรุ่นที่ key เรียกได้ (ดูหัวข้อตรวจปัญหา) แล้วตั้ง `GEMINI_MODEL`; ถ้าได้ 503 ให้ลองรุ่นอื่นหรือรอสักครู่ (ผู้ใช้ยังได้เทมเพลตและงานเข้าคิวลองใหม่)
- ตัวเลขคะแนนและเกณฑ์ระดับลีด (Cold/Warm/Hot, 50 คะแนนส่งต่อ) เป็นค่าเริ่มต้นที่เดา ยังไม่มีข้อมูลลีดจริงมายืนยัน ผู้ใช้แก้ได้ผ่าน API (ต้องรอพี่เลี้ยงตรวจ); กฎหลัก 4 ข้อมาจากพี่เลี้ยง (`is_core = true`) ที่เหลือเป็นกฎเสริมของทีมที่ปิดได้
- ตรวจจับ event จากข้อความใช้คำสำคัญ อาจจับผิด/พลาด (เช่น `หยุด` สำหรับเลิกรับข้อความ) แก้คำได้ที่ `PUT /event-keywords`; การให้ AI ช่วยจัดประเภทข้อความยังไม่ได้ทำ
- event ที่อิงเวลา (`no_reply_3d`, `inactive_7d`) ยังไม่มี job สร้าง กฎมีอยู่แล้วแต่ยังไม่ทำงานจนกว่าจะมี job
- ขั้นตอน PDPA ฝั่งลีดมีที่ API แล้ว (ต้องมี consent ก่อนบันทึก, ถอน consent, export/ลบ) แต่ยังไม่มีหน้า/ข้อความขอความยินยอมจริงใน LINE
- ยังไม่มี HTTPS (จะทำพร้อม deploy ด้วย Caddy) ห้ามนำไปใช้จริงบนอินเทอร์เน็ตก่อนมี HTTPS
- ลิมิตล็อกอินเก็บในหน่วยความจำของ API: รีสตาร์ท API แล้วนับใหม่ (`docker compose restart api`) จะย้ายไป Redis ตอน deploy; ตั้ง `PROXY_HEADER` เมื่ออยู่หลัง Caddy เพื่อให้ได้ IP จริง; ที่เก็บ token ฝั่งหน้าเว็บ (cookie httpOnly หรือ localStorage) ตัดสินใจตอนทำ React

## Run

    cp .env.example .env
    # แก้ .env: ตั้ง JWT_SECRET (32 ตัวขึ้นไป), ADMIN_EMAIL, ADMIN_PASSWORD (8 ถึง 72 ตัว)
    docker compose up --build

ใน PowerShell ใช้ `Copy-Item .env.example .env` แทน `cp`
สร้าง JWT_SECRET ใน PowerShell: `-join ((48..57)+(65..90)+(97..122) | Get-Random -Count 48 | % {[char]$_})`
ADMIN_EMAIL / ADMIN_PASSWORD ใช้สร้างแอดมินคนแรกตอนตารางผู้ใช้ยังว่างเท่านั้น (ห้าม commit `.env`)

ตารางลีดและกฎคะแนน (`db/leads.sql`): ถ้าสร้างฐานข้อมูลใหม่ให้ต่อท้าย `db/init.sql` หรือ `docker compose down -v` แล้วขึ้นใหม่ ถ้าใช้ฐานข้อมูลเดิมอยู่ ให้รันไฟล์นี้เพิ่ม (รันซ้ำได้: ตารางใช้ `IF NOT EXISTS`, seed กฎใส่เฉพาะตอนตาราง `scoring_rules` ยังว่าง, ค่าตั้งค่าและคำสำคัญไม่ซ้ำ):

    docker compose cp db\leads.sql db:/tmp/leads.sql
    docker compose exec db psql -U <DB_USER> -d <DB_NAME> -v ON_ERROR_STOP=1 -f /tmp/leads.sql

ห้ามส่งไฟล์ SQL ที่มีภาษาไทยผ่าน pipe ใน PowerShell (`Get-Content ... | psql`) เพราะภาษาไทยอาจกลายเป็น `?` ในฐานข้อมูล; ตรวจได้ด้วย `SELECT name, encode(convert_to(name,'UTF8'),'hex') FROM scoring_rules;` (ภาษาไทยที่ถูกต้องขึ้นต้นด้วย `e0`)

## Login และสิทธิ์

| Role | ทำอะไรได้ |
|---|---|
| admin | ทุกอย่าง + จัดการผู้ใช้ (`POST/GET /users`, `PATCH /users/:id`, `POST /users/:id/reset-password`, `POST /users/:id/reset-link`, `GET /users/approver-check`) + ดู `GET /audit-log` + คำนวณคะแนนใหม่/แก้เกณฑ์ส่งต่อ/ส่งออกและลบลีด |
| marketing | สร้าง/แก้แคมเปญ สินค้า คอนเทนต์ เรียก AI ส่งอนุมัติ/อนุมัติ/ปฏิเสธ + สร้างลีด บันทึก event/consent แก้กฎคะแนนและคำสำคัญ |
| sales | ดูข้อมูล (GET) + รับลีดที่ `qualified` และเปลี่ยนสถานะลีด (`handed_off` / `closed` / `lost`) |

PowerShell (ใช้ `Read-Host` เพื่อไม่พิมพ์รหัสผ่านลงในคำสั่ง/ประวัติ):

    $body = @{ email='admin@example.com'; password=(Read-Host 'password') } | ConvertTo-Json
    $r = Invoke-RestMethod -Method Post -Uri http://localhost:8080/auth/login -ContentType 'application/json' -Body $body
    $PSDefaultParameterValues['Invoke-RestMethod:Headers'] = @{ Authorization = "Bearer $($r.token)" }
    Invoke-RestMethod http://localhost:8080/auth/me

สร้างผู้ใช้ใหม่ (admin เท่านั้น):

    Invoke-RestMethod -Method Post -Uri http://localhost:8080/users -ContentType 'application/json' `
      -Body '{"email":"sale1@example.com","password":"ตั้งรหัสผ่านที่นี่","role":"sales"}'

**บัญชีสร้างโดย admin เท่านั้น** (ไม่มีสมัครเอง และไม่เชื่อมระบบของบริษัท) กำหนด role ตามหน้าที่ มี 2 วิธี:

1. **ลิงก์เชิญ (แนะนำ)**: ส่ง `POST /users` โดย**ไม่ใส่** `password` → ได้ `invite_url` (ใช้ได้ครั้งเดียว อายุ 72 ชม.) admin คัดลอกไปส่งให้เจ้าของบัญชี เขาตั้งรหัสผ่านเองที่ `POST /auth/set-password` (`token`, `new_password`)
2. **รหัสผ่านชั่วคราว**: ใส่ `password` → ผู้ใช้ต้องเปลี่ยนตอนล็อกอินครั้งแรก

ลืมรหัสผ่าน: admin เรียก `POST /users/:id/reset-link` → ได้ `reset_url` (ใช้ได้ครั้งเดียว อายุ 1 ชม.) ส่งให้เจ้าของบัญชี รหัสเดิมยังใช้ได้จนกว่าจะใช้ลิงก์ และลิงก์ใหม่จะยกเลิกลิงก์เก่า เมื่อตั้งรหัสใหม่แล้ว session เดิมหมดอายุทันที ระบบเก็บเฉพาะแฮช SHA-256 ของโทเค็น ไม่เก็บโทเค็นจริง

ข้อมูลผู้ใช้: `name`, `auth_provider` (ตอนนี้เป็น `local` เสมอ เผื่อ SSO ในอนาคต), `last_login_at`, `pending_link` (มีลิงก์ที่ยังไม่ถูกใช้/ไม่หมดอายุ) ตั้งค่าใน `.env`: `APP_BASE_URL` (ใช้สร้างลิงก์), `ALLOWED_EMAIL_DOMAINS` (จำกัดโดเมนอีเมล เว้นว่าง = ไม่จำกัด) `GET /users/approver-check` เตือนเมื่อมีผู้อนุมัติ (marketing/admin ที่เปิดใช้งาน) น้อยกว่า 2 คน เพราะผู้อนุมัติต้องคนละคนกับผู้สร้าง

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

ทดสอบลีดและคะแนน (ต้องรัน `db/leads.sql` และรวม `leads.go` เข้า `main.go` ก่อน; แคมเปญ id = 1 ผูกสินค้าราคา 12900):

    # สร้างลีด: ต้องส่ง consent_general=true และ consent_version ไม่งั้นได้ 400 และไม่บันทึกอะไร
    Invoke-RestMethod -Method Post -Uri http://localhost:8080/leads -ContentType 'application/json' `
      -Body '{"campaign_id":1,"display_name":"ลูกค้าทดสอบ","channel":"mock","consent_general":true,"consent_marketing":false,"consent_version":"v1"}'

    # ส่งข้อความลูกค้า: ระบบจับ event จากคำสำคัญ (ไม่เก็บข้อความ) แล้วคำนวณคะแนนใหม่
    # (ถ้าส่งภาษาไทยจาก PowerShell ใช้ UTF8.GetBytes เหมือนตัวอย่าง products)
    Invoke-RestMethod -Method Post -Uri http://localhost:8080/leads/1/message -ContentType 'application/json; charset=utf-8' `
      -Body ([System.Text.Encoding]::UTF8.GetBytes('{"text":"ขอรายละเอียดหน่อย ราคาเท่าไหร่ ช่วยโทรกลับด้วย"}'))

    # หรือบันทึก event ตรงๆ
    Invoke-RestMethod -Method Post -Uri http://localhost:8080/leads/1/events -ContentType 'application/json' `
      -Body '{"type":"replied_fast","data":{"minutes":10}}'

    Invoke-RestMethod http://localhost:8080/leads/1          # คะแนน ระดับ และเหตุผลของคะแนน
    Invoke-RestMethod "http://localhost:8080/leads?level=hot"
    Invoke-RestMethod http://localhost:8080/scoring-rules

    # ลองกฎโดยไม่บันทึก
    Invoke-RestMethod -Method Post -Uri http://localhost:8080/scoring-rules/test -ContentType 'application/json' `
      -Body '{"events":[{"type":"asked_price"},{"type":"gave_budget"}],"lead":{"budget":10000},"product_price":12900}'

    # แก้กฎแล้วต้องคำนวณลีดเดิมใหม่ (admin)
    Invoke-RestMethod -Method Post http://localhost:8080/scoring-rules/recalculate

ทดสอบระบบทั้งชุดอัตโนมัติ (ล็อกอิน สิทธิ์ เปลี่ยนรหัส ปิดผู้ใช้ audit log และ AI) ต้องรัน `docker compose up -d` ก่อน:

    .\scripts\smoke-test.ps1              # ถามรหัสผ่าน admin
    .\scripts\smoke-test.ps1 -RateLimit   # ทดสอบลิมิตล็อกอินด้วย (IP จะถูกบล็อก 15 นาที ให้ docker compose restart api หลังทดสอบ)

สคริปต์สร้างผู้ใช้ `smoke-*@example.com` แล้วปิดไว้ท้ายการทดสอบ ใช้กับฐานข้อมูลสำหรับพัฒนาเท่านั้น (ชุดทดสอบลีดแยกอยู่ที่ `scripts/leads-smoke-test.ps1`)

ทดสอบลีดและกฎคะแนน (ข้อมูลจำลอง; สร้างแล้ว anonymize ลีดและลบกฎชั่วคราวเมื่อจบ):

    .\scripts\leads-smoke-test.ps1

รันสคริปต์ทดสอบอัตโนมัติ (ต้อง `docker compose up -d` ก่อน; ใช้ฐานข้อมูลสำหรับพัฒนาเท่านั้น):

    Set-ExecutionPolicy -Scope Process -ExecutionPolicy Bypass   # ครั้งเดียวต่อหน้าต่าง PowerShell ถ้าสคริปต์ถูกบล็อก
    .\scripts\smoke-test.ps1

แต่ละรอบล็อกอินผิดโดยตั้งใจ 2 ครั้ง ถ้ารัน 2 รอบภายใน 15 นาทีให้ `docker compose restart api` ก่อน

รัน unit test (ไม่ต้องมี Go ในเครื่อง ใช้ Docker; รวมเทสต์ของ engine ให้คะแนนใน `scoring_test.go`):

    docker run --rm -v "${PWD}/backend:/app" -w /app golang:1.22 sh -c "go mod tidy && go test ./..."

## ลีดและการให้คะแนน

ลีด = ลูกค้าแต่ละคนที่แสดงความสนใจ (เช่น ทักมาถามราคา) ระบบ **จัดอันดับว่าใครพร้อมซื้อ** เพื่อให้ทีมขายติดต่อคนที่ปิดการขายได้ก่อน ไม่ได้วัดว่าช่องทางไหนมีคนเข้าดูเยอะ (ส่วนนั้นอยู่ในการวัดผล ขั้นที่ 8; ลีดเก็บ `channel` ไว้เพื่อสรุปตามช่องทางได้)

**ให้คะแนนด้วยกฎเป็นหลัก ไม่ใช้ AI ตัดสิน** (ลดค่าใช้จ่ายและอธิบายได้) กฎเก็บในตาราง `scoring_rules` เป็นข้อมูล แก้/เพิ่ม/ลบ/เปิด-ปิดได้ผ่าน API โดยไม่ต้อง deploy ใหม่ ทุกการแก้ลง audit log (ค่าก่อน-หลัง)

กฎหลัก 4 ข้อจากพี่เลี้ยง (`is_core = true`):

| กฎ | event | คะแนน |
|---|---|---|
| ทักขอรายละเอียด/ข้อมูลเพิ่มเติม | `asked_details` | +10 |
| ขอพูดคุยกับเจ้าหน้าที่ | `requested_human` | +30 ส่งต่อทันที |
| สอบถามราคา | `asked_price` | +10 |
| ให้ติดต่อกลับ | `requested_callback` | +30 ส่งต่อทันที |

กฎเสริมของทีม (ปิดได้): ถามสภาพ +10, ถามผ่อน +10, ถามจัดส่ง +5, ต่อรอง +10, ขอนัดดู +30 (ส่งต่อทันที), ระบุงบ +15, งบพอกับราคาสินค้า +10, งบต่ำกว่า 70% ของราคา −10, ตอบเร็วใน 1 ชม. +5 (ทุกครั้ง เพดาน +10), กลับมาคุยหลังหายไป 1 วัน +10, ไม่ตอบ 3 วัน −15, ไม่มีความเคลื่อนไหว 7 วัน −10 (ทุกครั้ง เพดาน −30)

ระดับลีด (ค่าตั้งต้น แก้ได้ที่ `PATCH /scoring-settings`):

| คะแนน | ระดับ | ระบบทำอะไร |
|---|---|---|
| 0 ถึง 24 | Cold | nurturing เบาๆ สัปดาห์ละ 1 ครั้ง สูงสุด 2 ครั้ง |
| 25 ถึง 49 | Warm | ติดตามทุก 2 ถึง 3 วัน สูงสุด 3 ครั้ง |
| 50 ขึ้นไป | Hot | เปลี่ยนเป็น `qualified` ส่งต่อทีมขาย |
| มี event ขอคุยกับคน / ให้ติดต่อกลับ / ขอนัดดู | Hot ทันที | ไม่ต้องรอถึง 50 |
| มี event `unsubscribed` | ตัดสิทธิ์ | ถอน `consent_marketing` หยุดติดตามทุกแบบ |

คะแนนรวมถูกจำกัดที่ 0 ถึง 100 · ติดตามเชิงรุกได้เฉพาะลีดที่ให้ `consent_marketing`

รูปแบบเงื่อนไขของกฎ (JSON เท่านั้น ไม่รันโค้ด): `event` (+ `where`), `lead` (ฟิลด์ของลีด), `all` / `any` / `not` ซ้อนได้ไม่เกิน 5 ชั้น; ตัวดำเนินการ `= != > >= < <= in contains`; ค่าอ้างอิง `{"ref":"product.price","multiply":0.7}` ตัวอย่าง:

    {"event":"replied_fast","where":{"field":"minutes","op":"<=","value":60}}
    {"lead":{"field":"budget","op":">=","value":{"ref":"product.price"}}}
    {"all":[{"event":"asked_price"},{"any":[{"event":"asked_installment"},{"event":"asked_negotiate"}]}]}

กติกา: ลีดต้องมี `consent_general` ก่อนบันทึก (ไม่งั้น 400) · งบที่ไม่ทราบเก็บเป็น NULL ไม่ใช่ 0 (กันถูกหักเป็น "งบต่ำ") · กฎที่เคยให้คะแนนแล้วลบแบบ soft (`active=false`) เพื่อให้ `lead_score_log` ย้อนดูได้ · ลีดที่ถอน consent หรือถูกลบ (anonymize) บันทึก event ไม่ได้ (409) · ไม่เก็บข้อความลูกค้าดิบ

Endpoints:

| Endpoint | ทำอะไร | role |
|---|---|---|
| `POST /leads` | สร้างลีด (ต้องมี consent) | marketing, admin |
| `GET /leads` (`?status=&level=&channel=`), `GET /leads/:id` | ดูลีด เรียงตามคะแนน พร้อมเหตุผลของคะแนน | ทุก role |
| `POST /leads/:id/events` | บันทึก event แล้วคำนวณคะแนนใหม่ | marketing, admin |
| `POST /leads/:id/message` | ส่งข้อความ ระบบจับ event ด้วยคำสำคัญแล้วคำนวณ (ไม่เก็บข้อความ) | marketing, admin |
| `POST /leads/:id/consent` | ให้/ถอนความยินยอม แยก general / marketing | marketing, admin |
| `POST /leads/:id/status` | `handed_off` / `closed` / `lost` | sales, admin |
| `GET /leads/:id/export`, `DELETE /leads/:id` | ส่งออก / ทำให้ไม่ระบุตัวตน (PDPA) | admin |
| `GET /scoring-rules` | ดูกฎ | ทุก role |
| `POST /scoring-rules`, `PATCH /scoring-rules/:id`, `DELETE /scoring-rules/:id` | เพิ่ม/แก้/ลบกฎ (ตรวจเงื่อนไขก่อนบันทึก) | marketing, admin |
| `POST /scoring-rules/test` | ลองกฎกับเหตุการณ์สมมติ ไม่บันทึก (ส่ง `rules` มาลองกฎใหม่ได้) | marketing, admin |
| `POST /scoring-rules/recalculate` | คำนวณลีดทั้งหมดใหม่หลังแก้กฎ | admin |
| `GET/PATCH /scoring-settings` | เกณฑ์ส่งต่อ ระดับ ความถี่ติดตาม | อ่านได้ทุก role, แก้ admin |
| `GET/PUT /event-keywords` | คำที่ใช้จับ event จากข้อความ | อ่านได้ทุก role, แก้ marketing/admin |

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

- db/init.sql: schema (products, campaigns, contents, legacy lead columns, lead/consent/scoring tables); ตาราง `users` สร้างตอน API เริ่มทำงาน
- db/leads.sql: ตารางลีด consent event กฎคะแนน คำสำคัญ + seed (กฎหลัก 4 ข้อของพี่เลี้ยง และกฎเสริม)
- backend/: Go (Fiber) API (`main.go` routes, `auth.go` login/roles, `ai.go` AI routes, `ai_jobs.go` ai_usage/โควตา/ตัวอัปเกรดงานคิว, `providers.go` Claude/Gemini, `fallback.go` เทมเพลตและ prompt, `jobs.go` + `redisq.go` คิวลองใหม่, `scoring.go` engine ให้คะแนนลีด (ไม่ต่อ DB), `scoring_test.go` unit test ของ engine, `leads.go` endpoint ลีดและกฎคะแนน)
- scripts/smoke-test.ps1: ทดสอบระบบเดิมผ่าน API (PowerShell)
- scripts/leads-smoke-test.ps1: ทดสอบ Lead API ด้วยข้อมูลจำลอง (PowerShell)
- scripts/demo-seed.ps1: สร้างข้อมูลจำลอง (สินค้า แคมเปญ ลีด Cold/Warm/Hot) สำหรับเดโม
- docs/DEMO_GUIDE.md: คู่มือเดโมและอัปเดตความคืบหน้า
- docs/LEADS_API.md + postman/: เอกสารและชุดทดสอบ API ลีด
- docker-compose.yml: PostgreSQL, Redis, API

## แนวปฏิบัติเมื่อใช้ AI ช่วยเขียนโค้ด

- อัปเดตส่วน "สถานะงาน" ทุกครั้งก่อน commit เพื่อให้เริ่มงานต่อได้แม้ limit เต็ม
- commit บ่อย แยกตามหน่วยงานเล็กๆ และ commit ก่อนให้ AI แก้ไฟล์ใหญ่
- ตรวจโค้ดที่ AI เขียน (`git diff`) ทุกครั้งก่อน commit; รันทั้ง `smoke-test.ps1` และ `leads-smoke-test.ps1` ก่อนทุกครั้งที่แก้ API
- ไม่ใส่ข้อมูลลูกค้าจริงหรือ API key ลงในเครื่องมือ AI; เก็บ key ใน `.env` (ถูก ignore โดย git)

## Next

1. หน้าเว็บขั้นต่ำ (ล็อกอิน, Manage Users, แคมเปญ, อนุมัติ, ลีด, แก้กฎ) — API ระบบบัญชีเสร็จแล้ว (ลิงก์เชิญ/ลืมรหัสผ่าน, ชื่อ, `auth_provider`) รอหน้าจอ
2. ปรับเกณฑ์คะแนนตามความเห็นพี่เลี้ยง; รายงานสรุปลีดตามช่องทาง
3. Daily Redis job for time-based events (`no_reply_3d`, `inactive_7d`) and nurturing
4. Chat channel interface with a mock channel, then LINE (own test OA) with PDPA consent
5. React + Vite frontend (including the scoring-rules management page), then HTTPS deployment (Caddy) with PROXY_HEADER set