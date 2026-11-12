# เชื่อม LINE Official Account กับ LeadLoop

ระบบรับข้อความจาก LINE ที่ `POST /public/line/webhook` ปิดอยู่จนกว่าจะตั้ง `LINE_CHANNEL_SECRET` และ `LINE_CHANNEL_ACCESS_TOKEN` ครบทั้งสองค่า

## 1. สร้างช่องทาง (ทำครั้งเดียว)
1. สร้าง LINE Official Account ที่ https://manager.line.biz
2. ใน Official Account Manager: Settings > Messaging API > Enable Messaging API (เลือก Provider)
3. ที่ https://developers.line.biz เปิดช่องทางที่ได้ แท็บ Basic settings คัดลอก **Channel secret** แท็บ Messaging API กด Issue **Channel access token (long-lived)**
4. Official Account Manager > Response settings: เปิด **Chat** และ **Webhooks**, ปิด **Auto-response messages** และ **Greeting message** (ให้ระบบตอบเอง) เซลส์ตอบลูกค้าจากหน้าลีดใน LeadLoop ได้ (ระบบส่งข้อความเข้า LINE ของลูกค้าให้)

## 2. ตั้งค่าในระบบ
ใส่ใน `.env` (ห้าม commit) แล้ว `docker compose up -d --force-recreate api`
```
LINE_CHANNEL_SECRET=...
LINE_CHANNEL_ACCESS_TOKEN=...
LINE_CAMPAIGN_ID=34      # เลขแคมเปญที่ลีด LINE ใหม่จะผูกด้วย (ไม่บังคับ)
```

## 3. ให้ LINE เรียกถึง API
LINE ต้องการ URL แบบ HTTPS สาธารณะ
- ชุด prod: `https://<โดเมน>/api/public/line/webhook`
- ทดลองบนเครื่อง: เปิดอุโมงค์ไปพอร์ต 8080 (เช่น `cloudflared tunnel --url http://localhost:8080`) แล้วใช้ `https://<ชื่ออุโมงค์>/public/line/webhook`

- ทดลองผ่าน ngrok ที่ชี้พอร์ต 5173 (หน้าเว็บ Vite): ใช้ `https://<โดเมน ngrok>/api/public/line/webhook` (มี `/api` นำหน้า เพราะ Vite ส่งต่อให้ API) ถ้าอุโมงค์ชี้พอร์ต 8080 ให้ใช้ `/public/line/webhook` ไม่มี `/api`

ใส่ URL ที่ LINE Developers > Messaging API > Webhook URL กด **Verify** ต้องขึ้น Success แล้วเปิด **Use webhook**

## 4. ทดสอบ
- ไม่ต้องใช้ LINE จริง: `.\scripts\line-webhook-smoke-test.ps1` (ต้องตั้ง `$env:SMOKE_LINE_SECRET` เป็นค่าเดียวกับ `LINE_CHANNEL_SECRET`)
- ของจริง: เพิ่มเพื่อนกับ Official Account > กด "ยินยอม" > ถามราคา > พิมพ์ "ขอคุยกับเจ้าหน้าที่" > ดูลีดช่องทาง LINE ในหน้า "ลีด"

## ข้อจำกัดตอนนี้
- ตอบเฉพาะแชต 1:1 และข้อความตัวอักษร (กลุ่มและสติกเกอร์ถูกข้าม)
- เซลส์ตอบจากหน้าลีด (กล่อง "ตอบลูกค้า") ระบบส่งเป็น push message เฉพาะลีดที่ยังยินยอมอยู่ ข้อความ push นับเป็นโควตาข้อความรายเดือนของบัญชี LINE ถ้าส่งไม่สำเร็จ (เช่น ลูกค้าบล็อกบัญชี หรือโควตาเต็ม) หน้าเว็บจะแจ้งและไม่บันทึกข้อความนั้น
- ระบบไม่ส่งข้อความหาลูกค้าที่ไม่ได้ยินยอมหรือถอนแล้ว
- ร่างข้อความติดตามที่อนุมัติแล้วของลีด LINE ถูกส่งอัตโนมัติ (ทุก 30 วินาที) ตรวจความยินยอมซ้ำก่อนส่งทุกครั้ง และหยุดส่งเมื่อโควตารายเดือนเหลือไม่เกิน `LINE_PUSH_RESERVE` (ค่าเริ่มต้น 20 เก็บไว้ให้เซลส์ตอบมือ) ดูโควตาที่ `GET /line/quota`
- ถ้า LINE ไม่ตอบเลย: เปิด http://127.0.0.1:4040 (ngrok) ดูรหัสของ `POST .../line/webhook` 404 = URL ผิด (ngrok ชี้พอร์ต 5173 ต้องมี `/api` นำหน้า) หรือ LINE ยังไม่เปิดใน API, 401 = Channel secret ไม่ตรง, 200 แต่ไม่ตอบ = ดู `docker compose logs api`
- ตั้งแต่ลีดเป็น "พร้อมส่งต่อ" หรือ "ส่งต่อเซลส์แล้ว" จนกว่าจะปิดการขายหรือไม่สำเร็จ บอทจะหยุดตอบลูกค้าคนนั้น ข้อความที่ลูกค้าพิมพ์จะถูกเก็บและขึ้นในบทสนทนาของหน้าลีดให้เซลส์ตอบต่อ (มีป้าย "ข้อความใหม่" ในรายการลีด) ลูกค้าพิมพ์ "ถอนความยินยอม" ได้เสมอ และข้อความที่เก็บไว้จะถูกลบเมื่อถอน
- ข้อความขอความยินยอมเวอร์ชัน v2 แจ้งเรื่องนี้แล้ว ถ้าใช้งานจริงให้ตั้ง `CONSENT_VERSION=v2` ให้ตรงและให้ Deputy Manager ตรวจถ้อยคำ
