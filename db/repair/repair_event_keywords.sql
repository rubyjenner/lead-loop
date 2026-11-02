DELETE FROM event_keywords WHERE keyword ~ '^\?+$';

INSERT INTO event_keywords (event_type, keyword) VALUES
  ('asked_details','รายละเอียด'),('asked_details','ข้อมูลเพิ่มเติม'),('asked_details','สนใจ'),
  ('requested_human','เจ้าหน้าที่'),('requested_human','คุยกับคน'),('requested_human','แอดมิน'),
  ('asked_price','ราคา'),('asked_price','เท่าไหร่'),('asked_price','กี่บาท'),
  ('requested_callback','ติดต่อกลับ'),('requested_callback','โทรกลับ'),('requested_callback','โทรหา'),
  ('asked_condition','สภาพ'),('asked_condition','แบตเตอรี่'),('asked_condition','แบต'),
  ('asked_installment','ผ่อน'),('asked_installment','ผ่อนได้'),
  ('asked_shipping','ส่งของ'),('asked_shipping','จัดส่ง'),('asked_shipping','ค่าส่ง'),
  ('asked_negotiate','ลดได้'),('asked_negotiate','ต่อได้'),('asked_negotiate','ลดหน่อย'),
  ('requested_visit','นัดดู'),('requested_visit','ขอดูเครื่อง'),('requested_visit','ไปดูที่ร้าน'),
  ('unsubscribed','หยุด'),('unsubscribed','เลิกรับ'),('unsubscribed','unsubscribe')
ON CONFLICT DO NOTHING;
