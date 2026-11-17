-- +goose Up
-- This is an explicit development sink, not evidence of patient notification delivery.
CREATE TABLE development_event_sink (
 event_id uuid PRIMARY KEY REFERENCES outbox(id),
 event_type text NOT NULL,
 observed_at timestamptz NOT NULL DEFAULT now()
);
-- +goose Down
DROP TABLE development_event_sink;
