# คู่มือ deploy LeadLoop (เซิร์ฟเวอร์เดียว + Caddy HTTPS)

> สถานะ: ไฟล์ชุดนี้ตรวจรูปแบบแล้ว (`docker compose -f docker-compose.prod.yml config`) แต่ **ยังไม่ได้ลองรันบนเซิร์ฟเวอร์จริง** เพราะยังไม่มีเซิร์ฟเวอร์และโดเมน (เรื่องที่โฮสต์ยังรอคุยกับพี่เลี้ยง) ใช้ข้อมูลทดสอบเท่านั้นจนกว่า Deputy Manager อนุมัติเรื่อง PDPA

## โครงสร้าง

```
อินเทอร์เน็ต → Caddy (พอร์ต 80/443, HTTPS อัตโนมัติ)
                 ├─ /api/*  → API (Go)  → PostgreSQL, Redis   (ทั้งสามอยู่ในเครือข่ายภายใน ไม่เปิดพอร์ตออกนอก)
                 └─ อื่นๆ   → หน้าเว็บ React (ไฟล์ static)
```

หน้าเว็บและ API อยู่โดเมนเดียวกัน (คำขอ `/api/...`) จึงไม่ต้องเปิด CORS

## สิ่งที่ต้องมี

- เซิร์ฟเวอร์ Linux ที่ลง Docker + Docker Compose (RAM 2 GB ขึ้นไป พอสำหรับทดลอง)
- ชื่อโดเมนที่ชี้ A record มาที่ IP ของเซิร์ฟเวอร์ และเปิดพอร์ต 80 กับ 443 (Caddy ขอใบรับรองจาก Let's Encrypt เอง ต้องเข้าถึงพอร์ต 80 ได้)
- ไม่มีโดเมน = ไม่มี HTTPS ห้ามนำไปใช้งานจริงบนอินเทอร์เน็ต ใช้ `SITE_ADDRESS=:80` ทดลองในเครื่องเท่านั้น

## ขั้นตอน

1. นำโค้ดขึ้นเซิร์ฟเวอร์ (`git clone`) แล้ว `cp .env.production.example .env`
2. กรอกค่าใน `.env` ให้ครบ: `SITE_ADDRESS`, `APP_BASE_URL`, `POSTGRES_PASSWORD` (24 ตัวขึ้นไป), `JWT_SECRET` (32 ตัวขึ้นไป), `ADMIN_EMAIL`, `ADMIN_PASSWORD`
   สร้างค่าสุ่มได้ด้วย `openssl rand -base64 36`
3. `docker compose -f docker-compose.prod.yml up -d --build`
4. เปิด `https://<โดเมน>` ล็อกอินด้วยแอดมินที่ตั้งไว้ แล้ว **เปลี่ยนรหัสผ่านทันที** และเชิญผู้ใช้ที่หน้า "ผู้ใช้"
5. ตั้งสำรองฐานข้อมูลและทดสอบกู้คืน (ด้านล่าง)

ลองในเครื่องก่อนได้: ตั้ง `SITE_ADDRESS=:80` และ `APP_BASE_URL=http://localhost` แล้วเปิด `http://localhost` (ใช้ HTTP จึงเป็นการลองเท่านั้น)

## สำรองและกู้คืนฐานข้อมูล

```bash
./scripts/backup.sh          # สร้าง backups/leadloop-วันที่_เวลา.sql.gz เก็บ 7 วัน
./scripts/restore-test.sh    # ทดสอบกู้ไฟล์ล่าสุดลงฐานข้อมูลชั่วคราว เทียบจำนวนแถว แล้วลบทิ้ง (ไม่แตะข้อมูลจริง)
```

ตั้ง cron ให้สำรองทุกวัน 02:30: `30 2 * * * cd /opt/lead-loop && ./scripts/backup.sh >> backups/backup.log 2>&1`
และต้องรัน `restore-test.sh` อย่างน้อย 1 ครั้งหลังสำรองครั้งแรก (สำรองที่ไม่เคยกู้ยังไม่ถือว่ามีสำรอง) แล้วคัดลอกโฟลเดอร์ `backups/` ไปเก็บนอกเซิร์ฟเวอร์ด้วย ไม่งั้นดิสก์เสียก็หายทั้งหมด

ลองบนเครื่อง Windows: `.\scripts\backup.ps1` และ `.\scripts\restore-test.ps1` (ใช้ `docker-compose.yml` ปกติ)

กู้คืนจริง (ข้อมูลปัจจุบันจะถูกแทนที่ ทำเมื่อจำเป็นจริงเท่านั้น):

```bash
docker compose -f docker-compose.prod.yml stop api
gzip -dc backups/leadloop-XXXX.sql.gz | docker compose -f docker-compose.prod.yml exec -T db sh -c 'dropdb -U "$POSTGRES_USER" "$POSTGRES_DB" && createdb -U "$POSTGRES_USER" "$POSTGRES_DB" && psql -q -U "$POSTGRES_USER" "$POSTGRES_DB"'
docker compose -f docker-compose.prod.yml start api
```

## อัปเดตระบบ

```bash
git pull
docker compose -f docker-compose.prod.yml up -d --build
```

ข้อมูลอยู่ใน volume `dbdata` จึงไม่หายตอนอัปเดต (อย่าใช้ `down -v` เพราะจะลบข้อมูล) ตารางใหม่ที่โค้ดสร้างตอนเริ่มทำงาน (เช่น `users`, `daily_runs`, `lead_followups`) ถูกสร้างอัตโนมัติ ส่วนการแก้ `db/init.sql` ใช้กับฐานข้อมูลใหม่เท่านั้น

## เช็กลิสต์ก่อนเปิดให้คนอื่นใช้

- [ ] เข้าผ่าน `https://` ได้ (กุญแจในเบราว์เซอร์) และ `http://` เด้งไป https
- [ ] พอร์ต 5432, 6379, 8080 เข้าจากภายนอกไม่ได้ (ตรวจด้วย `nmap` หรือเครื่องมือสแกนพอร์ตออนไลน์)
- [ ] เปลี่ยนรหัสผ่านแอดมินแล้ว และ `.env` ไม่ถูก commit
- [ ] `restore-test.sh` ผ่าน และมีสำเนา backup นอกเซิร์ฟเวอร์
- [ ] มีผู้อนุมัติอย่างน้อย 2 คน (หน้า "ผู้ใช้" จะเตือนถ้าไม่ครบ)
- [ ] ล็อกอินผิดซ้ำแล้วได้ 429 จริง (ลิมิตทำงานด้วย IP ผู้ใช้จริง ไม่ใช่ IP ของ Caddy)
- [ ] Deputy Manager อนุมัติ PDPA แล้ว (ดู `docs/PDPA_SUMMARY.md`) ก่อนใส่ข้อมูลลูกค้าจริง
- [ ] `.env` มีตัวแปรครบ: ตรวจว่า `docker compose -f docker-compose.prod.yml config` ไม่ฟ้องว่าขาด `JWT_SECRET` หรือ `APP_BASE_URL` และคัดลอกตัวแปรทุกตัวจาก `.env.production.example` มาแล้ว (ตัวแปรของ LINE รูปสินค้า โควตา การมอบหมายลีด การเก็บข้อมูล และเวอร์ชันความยินยอมเพิ่มเข้าชุด prod แล้ว ก่อนหน้านี้ถ้าไม่ได้ส่งผ่าน compose โค้ดจะใช้ค่าเริ่มต้นเงียบๆ เช่น ตั้ง `RETENTION_*` ใน `.env` แต่ API ไม่เห็นค่า)
- [ ] ถ้าเปิดแชตสาธารณะหรือ LINE: ตั้ง `PUBLIC_CHAT_ENABLED=1` และ `APP_BASE_URL` (หรือ `LINE_IMAGE_BASE_URL`) เป็นที่อยู่ `https://` จริง LINE ดึงรูปสินค้าจากที่อยู่นี้ และ Webhook URL ต้องมี `/api` (`https://<โดเมน>/api/public/line/webhook`)
- [ ] `RETENTION_INACTIVE_DAYS` และ `RETENTION_WITHDRAWN_DAYS` ยังเป็น `0` (ปิด) จนกว่าบริษัทกำหนดระยะเวลา แล้วให้แอดมินดูตัวอย่างก่อนลบที่หน้าตั้งค่า

## ข้อจำกัดที่ยังมี

- เซิร์ฟเวอร์เดียว ไม่มี failover; Redis ไม่ได้ตั้งรหัสผ่านเพราะอยู่ในเครือข่ายภายในเท่านั้น (ห้ามเปิดพอร์ตออกนอก)
- ลิมิตล็อกอินเก็บในหน่วยความจำของ API (รีสตาร์ทแล้วนับใหม่) ยังไม่ย้ายไป Redis
- ยังไม่มีระบบเฝ้าดู/แจ้งเตือนเมื่อระบบล่ม (ใช้ `docker compose logs` ดูเอง)
- LINE จริงยังไม่เชื่อม (ต้องมี LINE OA ของตัวเอง และ HTTPS จากชุดนี้)
