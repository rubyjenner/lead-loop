-- LeadLoop: leads + consent + rule-based scoring
-- ใช้กับฐานข้อมูลใหม่: ต่อท้าย db/init.sql   |   ฐานข้อมูลเดิม: docker compose exec -T db psql -U <user> <db> < db/leads.sql
-- (ไม่ใส่ FOREIGN KEY ไป users เพราะตาราง users ถูกสร้างตอน API เริ่มทำงาน)

CREATE TABLE IF NOT EXISTS leads (
  id BIGSERIAL PRIMARY KEY,
  campaign_id BIGINT REFERENCES campaigns(id),
  display_name TEXT NOT NULL,
  channel TEXT NOT NULL,                       -- line | mock | web
  channel_user_id TEXT,
  budget NUMERIC,                              -- NULL = ไม่ทราบ (ห้ามใช้ 0 แทน)
  status TEXT NOT NULL DEFAULT 'new',          -- new | nurturing | qualified | handed_off | closed | lost
  score INT NOT NULL DEFAULT 0,
  level TEXT NOT NULL DEFAULT 'cold',          -- cold | warm | hot | disqualified
  consent_general BOOLEAN NOT NULL DEFAULT FALSE,
  consent_marketing BOOLEAN NOT NULL DEFAULT FALSE,
  consent_version TEXT NOT NULL,
  consent_at TIMESTAMPTZ NOT NULL,
  consent_withdrawn_at TIMESTAMPTZ,
  anonymized_at TIMESTAMPTZ,
  created_by TEXT,                             -- อีเมลผู้สร้าง (ตรงกับ contents.created_by)
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- Upgrade path for the legacy leads table created by db/init.sql.
-- Keep old columns (name/contact/consent) so existing application code/data is not destructively changed.
ALTER TABLE leads ADD COLUMN IF NOT EXISTS display_name TEXT;
ALTER TABLE leads ADD COLUMN IF NOT EXISTS channel_user_id TEXT;
ALTER TABLE leads ADD COLUMN IF NOT EXISTS budget NUMERIC;
ALTER TABLE leads ADD COLUMN IF NOT EXISTS level TEXT NOT NULL DEFAULT 'cold';
ALTER TABLE leads ADD COLUMN IF NOT EXISTS consent_general BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE leads ADD COLUMN IF NOT EXISTS consent_marketing BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE leads ADD COLUMN IF NOT EXISTS consent_version TEXT;
ALTER TABLE leads ADD COLUMN IF NOT EXISTS consent_at TIMESTAMPTZ;
ALTER TABLE leads ADD COLUMN IF NOT EXISTS consent_withdrawn_at TIMESTAMPTZ;
ALTER TABLE leads ADD COLUMN IF NOT EXISTS anonymized_at TIMESTAMPTZ;
ALTER TABLE leads ADD COLUMN IF NOT EXISTS created_by TEXT;
ALTER TABLE leads ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT now();

-- Backfill display names, but do NOT migrate the old boolean `consent` into valid consent.
-- The legacy flag has no versioned consent evidence; require explicit consent in the new flow.
UPDATE leads
SET display_name = COALESCE(NULLIF(display_name, ''), NULLIF(name, ''), '(unknown)'),
    updated_at = COALESCE(updated_at, created_at);
ALTER TABLE leads ALTER COLUMN display_name SET DEFAULT '(unknown)';
ALTER TABLE leads ALTER COLUMN display_name SET NOT NULL;

-- The legacy column used an enum that did not contain 'closed'. The API supports an extensible
-- status vocabulary, so convert it to TEXT while preserving current values.
ALTER TABLE leads ALTER COLUMN status DROP DEFAULT;
ALTER TABLE leads ALTER COLUMN status TYPE TEXT USING status::text;
ALTER TABLE leads ALTER COLUMN status SET DEFAULT 'new';

CREATE INDEX IF NOT EXISTS leads_campaign_idx ON leads(campaign_id);
CREATE INDEX IF NOT EXISTS leads_score_idx ON leads(score DESC);

CREATE TABLE IF NOT EXISTS consent_events (
  id BIGSERIAL PRIMARY KEY,
  lead_id BIGINT NOT NULL REFERENCES leads(id),
  kind TEXT NOT NULL,                          -- general | marketing
  action TEXT NOT NULL,                        -- granted | withdrawn
  text_version TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS lead_events (
  id BIGSERIAL PRIMARY KEY,
  lead_id BIGINT NOT NULL REFERENCES leads(id),
  type TEXT NOT NULL,
  data JSONB NOT NULL DEFAULT '{}',            -- ไม่เก็บข้อความลูกค้าดิบ เก็บเฉพาะค่าที่กฎต้องใช้ เช่น minutes
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS lead_events_lead_idx ON lead_events(lead_id);

CREATE TABLE IF NOT EXISTS scoring_rules (
  id BIGSERIAL PRIMARY KEY,
  name TEXT NOT NULL,
  description TEXT,
  category TEXT NOT NULL DEFAULT 'intent',     -- intent | fit | engagement | negative
  is_core BOOLEAN NOT NULL DEFAULT FALSE,      -- TRUE = เกณฑ์หลักจากพี่เลี้ยง, FALSE = กฎเสริมของทีม
  condition JSONB NOT NULL,
  points INT NOT NULL,
  mode TEXT NOT NULL DEFAULT 'once' CHECK (mode IN ('once','each')),
  max_total INT,
  active BOOLEAN NOT NULL DEFAULT TRUE,
  sort_order INT NOT NULL DEFAULT 0,
  updated_by TEXT,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS scoring_settings (
  key TEXT PRIMARY KEY,
  value JSONB NOT NULL
);

-- คำที่ใช้จับเหตุการณ์จากข้อความลูกค้า (แก้ได้ ภาษาไทยไม่มีเว้นวรรคจึงใช้การค้นหาข้อความย่อย)
CREATE TABLE IF NOT EXISTS event_keywords (
  id BIGSERIAL PRIMARY KEY,
  event_type TEXT NOT NULL,
  keyword TEXT NOT NULL,
  UNIQUE (event_type, keyword)
);

CREATE TABLE IF NOT EXISTS lead_score_log (
  id BIGSERIAL PRIMARY KEY,
  lead_id BIGINT NOT NULL REFERENCES leads(id),
  rule_id BIGINT,
  rule_name TEXT,
  points INT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS lead_score_log_lead_idx ON lead_score_log(lead_id);

-- ===== Seed: กฎหลัก 4 ข้อจากพี่เลี้ยง + กฎเสริมของทีม (ใส่เฉพาะตอนตารางกฎยังว่าง รันไฟล์ซ้ำได้) =====
DO $seed$
BEGIN
IF NOT EXISTS (SELECT 1 FROM scoring_rules) THEN
INSERT INTO scoring_rules (name, description, category, is_core, condition, points, mode, max_total, sort_order) VALUES
('ทักขอรายละเอียด/ข้อมูลเพิ่มเติม', 'เกณฑ์หลักข้อ 1', 'intent', TRUE, '{"event":"asked_details"}', 10, 'once', NULL, 10),
('ขอพูดคุยกับเจ้าหน้าที่', 'เกณฑ์หลักข้อ 2 ส่งต่อทันที', 'intent', TRUE, '{"event":"requested_human"}', 30, 'once', NULL, 20),
('สอบถามราคา', 'เกณฑ์หลักข้อ 3', 'intent', TRUE, '{"event":"asked_price"}', 10, 'once', NULL, 30),
('ให้ติดต่อกลับ', 'เกณฑ์หลักข้อ 4 ส่งต่อทันที', 'intent', TRUE, '{"event":"requested_callback"}', 30, 'once', NULL, 40);

-- กฎเสริมของทีม (ปิดได้จากหน้าแก้กฎ)
INSERT INTO scoring_rules (name, description, category, is_core, condition, points, mode, max_total, sort_order) VALUES
('ถามสภาพ/แบตเตอรี่', NULL, 'intent', FALSE, '{"event":"asked_condition"}', 10, 'once', NULL, 110),
('ถามผ่อน/ช่องทางชำระ', NULL, 'intent', FALSE, '{"event":"asked_installment"}', 10, 'once', NULL, 120),
('ถามการจัดส่ง', NULL, 'intent', FALSE, '{"event":"asked_shipping"}', 5, 'once', NULL, 130),
('ต่อรองราคา', NULL, 'intent', FALSE, '{"event":"asked_negotiate"}', 10, 'once', NULL, 140),
('ขอนัดดูเครื่อง', 'ส่งต่อทันที', 'intent', FALSE, '{"event":"requested_visit"}', 30, 'once', NULL, 150),
('ระบุงบ', NULL, 'fit', FALSE, '{"event":"gave_budget"}', 15, 'once', NULL, 210),
('งบพอกับราคาสินค้า', 'งบ >= ราคา', 'fit', FALSE, '{"lead":{"field":"budget","op":">=","value":{"ref":"product.price"}}}', 10, 'once', NULL, 220),
('งบต่ำกว่า 70% ของราคา', NULL, 'fit', FALSE, '{"lead":{"field":"budget","op":"<","value":{"ref":"product.price","multiply":0.7}}}', -10, 'once', NULL, 230),
('ตอบกลับเร็ว', 'ภายใน 60 นาที', 'engagement', FALSE, '{"event":"replied_fast","where":{"field":"minutes","op":"<=","value":60}}', 5, 'each', 10, 310),
('กลับมาคุยหลังหายไป 1 วัน', NULL, 'engagement', FALSE, '{"event":"returned_after_1d"}', 10, 'once', NULL, 320),
('ไม่ตอบ 3 วัน', 'ต้องมี job ตรวจรายวัน', 'negative', FALSE, '{"event":"no_reply_3d"}', -15, 'once', NULL, 410),
('ไม่มีความเคลื่อนไหว 7 วัน', 'ต้องมี job ตรวจรายวัน', 'negative', FALSE, '{"event":"inactive_7d"}', -10, 'each', 30, 420);
END IF;
END
$seed$;

INSERT INTO scoring_settings (key, value) VALUES
  ('handoff_threshold', '50'),
  ('level_warm_min', '25'),
  ('score_min', '0'),
  ('score_max', '100'),
  ('immediate_handoff_events', '["requested_human","requested_callback","requested_visit"]'),
  ('disqualify_events', '["unsubscribed"]'),
  ('followup_cold', '{"every_days":7,"max":2}'),
  ('followup_warm', '{"every_days":3,"max":3}')
ON CONFLICT (key) DO NOTHING;

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
