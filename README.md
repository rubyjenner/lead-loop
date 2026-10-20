# LeadLoop

AI Agent for marketing and sales: plan campaigns, generate content, publish after human approval,
nurture and score leads, hand qualified leads to sales, and feed results back into the next plan.

## Run

    cp .env.example .env
    docker compose up --build
    curl localhost:8080/health

## Try the first endpoints

    curl -X POST localhost:8080/campaigns -H 'Content-Type: application/json' \
      -d '{"name":"Launch","goal":"50 leads","target_audience":"students","budget":10000}'
    curl localhost:8080/campaigns

## Structure

- db/init.sql: schema (products, campaigns, contents, leads, messages, metrics)
- backend/: Go (Fiber) API
- docker-compose.yml: PostgreSQL, Redis, API

## Next

1. Content endpoints (create, list, submit for approval)
2. Claude API: campaign plan and content drafts
3. Leads and chat (LINE), lead scoring
4. React + Vite frontend

# LeadLoop

AI Agent for marketing and sales: plan campaigns, generate content, publish after human approval,
nurture and score leads, hand qualified leads to sales, and feed results back into the next plan.

## สถานะงาน (อัปเดตทุกครั้งก่อน commit)

อัปเดตล่าสุด: 20 ต.ค. 2569 (เย็น) · เอกสารดีไซน์ฉบับปรับปรุง: AI_Agent_Draft_Design_v2.pdf

**ทำเสร็จแล้ว** (ทดสอบแล้วบนเครื่อง)
- schema ฐานข้อมูล (`db/init.sql`), docker-compose (PostgreSQL, Redis, API), `go.sum`
- API: `GET /health`, `POST/GET /campaigns`
- Products: `POST/GET /products`, แคมเปญผูกสินค้าได้ด้วย `product_id`
- Content flow: `POST/GET /campaigns/:id/contents`, `POST /contents/:id/submit`, `/approve`, `/reject` (รวมกรณี error 404/409)
- AI (โหมดจำลองเมื่อไม่มี API key): `POST /campaigns/:id/plan`, `POST /campaigns/:id/generate-contents`
- JSON ตอบกลับเป็น UTF-8 (ภาษาไทยแสดงถูกต้อง)

**กำลังทำ**
- ยังไม่มี (งานถัดไปเริ่ม 21 ต.ค.)

**ขั้นต่อไป** (ตามเอกสารดีไซน์ฉบับปรับปรุง 20 ต.ค.)
1. 21 ต.ค.: ระบบล็อกอินและแยกสิทธิ์ (admin / การตลาด / ทีมขาย) ผู้อนุมัติมาจากผู้ที่ล็อกอิน และปิดช่องโหว่ (พอร์ต DB/Redis, ข้อความ error)
2. 22 ถึง 23 ต.ค.: AI สำรอง (Claude → Gemini → กฎ/เทมเพลต → คิว Redis), บันทึกการใช้งาน `ai_usage` และโควตา
3. 26 ต.ค.: Postman collection และเอกสาร API
4. ขอจากบริษัท: Claude API key (หรือยืนยันผู้ให้บริการ AI), LINE OA + Messaging API, server + โดเมน, นโยบายความเป็นส่วนตัว (PDPA)
5. พ.ย. เป็นต้นไป: ต่อ AI จริง, Frontend (React), LINE พร้อมขอความยินยอม PDPA, ให้คะแนนลีดด้วยกฎ

**ปัญหาที่ค้าง / ข้อจำกัดปัจจุบัน**
- ยังไม่ได้ Claude API key (`ANTHROPIC_API_KEY` ว่างอยู่) AI จึงทำงานในโหมดจำลอง
- ยังไม่มี AI สำรองจริง, `ai_usage`/โควตา และขั้นตอน PDPA (วางแผนไว้แล้ว ดูขั้นต่อไป)
- ยังไม่มีระบบล็อกอิน: ทุก endpoint เปิดโล่ง และ `approved_by` เป็นข้อความที่ส่งมาเอง ห้ามนำไปใช้จริงก่อนทำล็อกอิน
- พอร์ต 5432 (DB) และ 6379 (Redis) เปิดสู่ภายนอก ใช้เฉพาะตอนพัฒนา

## Run

    cp .env.example .env
    docker compose up --build

ใน PowerShell ใช้ `Copy-Item .env.example .env` แทน `cp`

## Try the first endpoints

macOS / Linux / Git Bash:

    curl localhost:8080/health
    curl -X POST localhost:8080/campaigns -H 'Content-Type: application/json' \
      -d '{"name":"Launch","goal":"50 leads","target_audience":"students","budget":10000}'
    curl localhost:8080/campaigns

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
    Invoke-RestMethod -Method Post -Uri http://localhost:8080/contents/1/approve `
      -ContentType 'application/json' -Body '{"approved_by":"mentor"}'

ทดสอบ products:

    Invoke-RestMethod -Method Post -Uri http://localhost:8080/products `
      -ContentType 'application/json; charset=utf-8' `
      -Body ([System.Text.Encoding]::UTF8.GetBytes('{"name":"iPhone 12","description":"มือสอง สภาพ 90%","price":12900,"brand_info":"ร้านมือถือ"}'))
    Invoke-RestMethod http://localhost:8080/products
    Invoke-RestMethod -Method Post -Uri http://localhost:8080/campaigns `
      -ContentType 'application/json' -Body '{"name":"Phone sale","product_id":1,"budget":5000}'

ทดสอบ AI (โหมดจำลอง ถ้า `ANTHROPIC_API_KEY` ว่าง ผลจะขึ้น `"mock": true`):

    Invoke-RestMethod -Method Post http://localhost:8080/campaigns/1/plan
    Invoke-RestMethod -Method Post -Uri http://localhost:8080/campaigns/1/generate-contents `
      -ContentType 'application/json' -Body '{"channel":"line","count":3}'
    Invoke-RestMethod http://localhost:8080/campaigns/1/contents

## ตรวจปัญหาเบื้องต้น

    docker compose ps
    docker compose logs api
    docker compose down -v   # ล้าง volume ฐานข้อมูล เมื่อแก้ db/init.sql (init.sql รันเฉพาะตอนสร้าง volume ครั้งแรก)

## หลักการออกแบบสำคัญ (รายละเอียดในเอกสาร Draft Design หัวข้อ 8 และ 9)

- **AI สำรอง:** ถ้า AI ล่ม ช้า หรือเต็มลิมิต ระบบตกไปใช้ AI ตัวที่สอง แล้วอัลกอริทึมกฎ/เทมเพลต แล้วเข้าคิว Redis ลองใหม่ ทุกผลลัพธ์บันทึก `generated_by` และต้องผ่านการอนุมัติจากคนเหมือนกัน (โหมดจำลอง `mock` ที่มีอยู่ตอนนี้ใช้ทดสอบเท่านั้น ไม่ใช่ตัวสำรอง)
- **ลิมิตและค่าใช้จ่าย AI:** ตั้งเพดานที่ผู้ให้บริการ, บันทึกการใช้งาน (`ai_usage`), โควตาต่อวัน, จำกัดความยาวคำตอบ, เก็บผลลัพธ์ไว้ไม่เรียกซ้ำโดยไม่จำเป็น, ให้คะแนนลีดด้วยกฎเป็นหลัก
- **PDPA:** ขอความยินยอมก่อนเก็บข้อมูลและก่อนส่งให้ AI, เก็บหลักฐานความยินยอม, แยกความยินยอมด้านการตลาด, มีช่องทางลบ/ส่งออกข้อมูลลีด, ปกปิดข้อมูลก่อนส่งให้ AI ภายนอก, ไม่บันทึกข้อมูลลูกค้าลง log ช่วงพัฒนาใช้ข้อมูลจำลองเท่านั้น (ฝ่ายกฎหมายของบริษัทตรวจก่อนใช้งานจริง)

## Structure

- db/init.sql: schema (products, campaigns, contents, leads, messages, metrics)
- backend/: Go (Fiber) API
- docker-compose.yml: PostgreSQL, Redis, API

## แนวปฏิบัติเมื่อใช้ AI ช่วยเขียนโค้ด

- อัปเดตส่วน "สถานะงาน" ทุกครั้งก่อน commit เพื่อให้เริ่มงานต่อได้แม้ limit เต็ม
- commit บ่อย แยกตามหน่วยงานเล็กๆ และ commit ก่อนให้ AI แก้ไฟล์ใหญ่
- ตรวจโค้ดที่ AI เขียน (`git diff`) ทุกครั้งก่อนรวมเข้าโปรเจกต์
- ไม่ใส่ข้อมูลลูกค้าจริงหรือ API key ลงในเครื่องมือ AI; เก็บ key ใน `.env` (ถูก ignore โดย git)

## Next

1. Login and roles, AI fallback (Claude → Gemini → rules/templates), usage tracking and quotas
2. Claude/Gemini API: campaign plan and content drafts (real calls once a key is available)
3. Leads and chat (LINE) with PDPA consent, rule-based lead scoring
4. React + Vite frontend