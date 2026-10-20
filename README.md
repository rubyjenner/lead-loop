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

อัปเดตล่าสุด: 20 ต.ค. 2569

**ทำเสร็จแล้ว**
- schema ฐานข้อมูล (`db/init.sql`)
- docker-compose (PostgreSQL, Redis, API)
- API: `GET /health`, `POST /campaigns`, `GET /campaigns`, `POST /contents/:id/approve`

**กำลังทำ**
- ทดสอบ `docker compose up --build` และ endpoint ทั้งหมด

**ขั้นต่อไป**
1. เพิ่ม `go.sum` (รัน `go mod tidy` ในโฟลเดอร์ `backend`)
2. Content endpoints: สร้าง, ดูรายการ, ส่งขออนุมัติ (`submit`)
3. ต่อ Claude API: วางแผนแคมเปญและสร้างคอนเทนต์ (เริ่ม 27 ต.ค.)

**ปัญหาที่ค้าง**
- `POST /contents/:id/approve` ตอบ 409 เสมอ เพราะยังไม่มี endpoint สร้างและส่ง content เข้า `pending_approval`
- ยังไม่ได้ Claude API key (`ANTHROPIC_API_KEY` ว่างอยู่)

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

## ตรวจปัญหาเบื้องต้น

    docker compose ps
    docker compose logs api
    docker compose down -v   # ล้าง volume ฐานข้อมูล เมื่อแก้ db/init.sql (init.sql รันเฉพาะตอนสร้าง volume ครั้งแรก)

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

1. Content endpoints (create, list, submit for approval)
2. Claude API: campaign plan and content drafts
3. Leads and chat (LINE), lead scoring
4. React + Vite frontend