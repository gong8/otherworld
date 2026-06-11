CREATE TABLE IF NOT EXISTS voices (
  id         text PRIMARY KEY,
  scope      text NOT NULL,
  charter    jsonb NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS utterances (
  seq     bigserial PRIMARY KEY,
  id      text UNIQUE NOT NULL,
  ts      timestamptz NOT NULL,
  scope   text NOT NULL,
  payload jsonb NOT NULL
);
CREATE INDEX IF NOT EXISTS utterances_scope_seq ON utterances (scope, seq);

CREATE TABLE IF NOT EXISTS exchanges (
  id           text PRIMARY KEY,
  scope        text NOT NULL,
  state        text NOT NULL CHECK (state IN ('open','settled','abandoned','interrupted')),
  participants text[] NOT NULL,
  opened_at    timestamptz NOT NULL,
  closed_at    timestamptz
);

CREATE TABLE IF NOT EXISTS settlements (
  id          text PRIMARY KEY,
  exchange_id text NOT NULL REFERENCES exchanges(id),
  scope       text NOT NULL,
  terms       jsonb NOT NULL,
  parties     text[] NOT NULL,
  ts          timestamptz NOT NULL
);

-- law 7: the door forgets. rows past expires_at are purged.
CREATE TABLE IF NOT EXISTS presence_events (
  id         text PRIMARY KEY,
  scope      text NOT NULL,
  voice      text NOT NULL,
  event      text NOT NULL CHECK (event IN ('entered','left')),
  ts         timestamptz NOT NULL,
  expires_at timestamptz NOT NULL
);
