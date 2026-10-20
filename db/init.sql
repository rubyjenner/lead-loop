CREATE TYPE campaign_status AS ENUM ('draft','planned','running','finished');
CREATE TYPE content_status  AS ENUM ('draft','pending_approval','approved','published','rejected');
CREATE TYPE lead_status     AS ENUM ('new','nurturing','qualified','handed_off','won','lost');

CREATE TABLE products (
  id          BIGSERIAL PRIMARY KEY,
  name        TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  price       NUMERIC(12,2),
  brand_info  TEXT NOT NULL DEFAULT '',
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE campaigns (
  id              BIGSERIAL PRIMARY KEY,
  product_id      BIGINT REFERENCES products(id),
  name            TEXT NOT NULL,
  goal            TEXT NOT NULL DEFAULT '',
  target_audience TEXT NOT NULL DEFAULT '',
  budget          NUMERIC(12,2) NOT NULL DEFAULT 0,
  plan            TEXT NOT NULL DEFAULT '',
  status          campaign_status NOT NULL DEFAULT 'draft',
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE contents (
  id          BIGSERIAL PRIMARY KEY,
  campaign_id BIGINT NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
  channel     TEXT NOT NULL,
  body        TEXT NOT NULL,
  status      content_status NOT NULL DEFAULT 'draft',
  approved_by TEXT,
  approved_at TIMESTAMPTZ,
  publish_at  TIMESTAMPTZ,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE leads (
  id          BIGSERIAL PRIMARY KEY,
  campaign_id BIGINT REFERENCES campaigns(id),
  name        TEXT NOT NULL DEFAULT '',
  contact     TEXT NOT NULL DEFAULT '',
  channel     TEXT NOT NULL DEFAULT '',
  score       INT NOT NULL DEFAULT 0,
  status      lead_status NOT NULL DEFAULT 'new',
  consent     BOOLEAN NOT NULL DEFAULT false,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE messages (
  id         BIGSERIAL PRIMARY KEY,
  lead_id    BIGINT NOT NULL REFERENCES leads(id) ON DELETE CASCADE,
  role       TEXT NOT NULL CHECK (role IN ('lead','ai','human')),
  body       TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE metrics (
  id          BIGSERIAL PRIMARY KEY,
  campaign_id BIGINT NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
  day         DATE NOT NULL,
  reach       INT NOT NULL DEFAULT 0,
  clicks      INT NOT NULL DEFAULT 0,
  spend       NUMERIC(12,2) NOT NULL DEFAULT 0,
  leads       INT NOT NULL DEFAULT 0,
  closed      INT NOT NULL DEFAULT 0,
  UNIQUE (campaign_id, day)
);

CREATE INDEX idx_contents_campaign ON contents(campaign_id);
CREATE INDEX idx_leads_campaign ON leads(campaign_id);
CREATE INDEX idx_messages_lead ON messages(lead_id);
