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
- บอทหยุดตอบลูกค้าคนนั้นเมื่อเซลส์รับลีด (สถานะ ส่งต่อแล้ว) หรือเมื่อเซลส์ตอบลูกค้าไปแล้ว ข้อความของลูกค้ายังถูกจับเหตุการณ์และให้คะแนนต่อ และลูกค้าพิมพ์ "ถอนความยินยอม" ได้เสมอ
