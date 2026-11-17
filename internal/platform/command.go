package platform

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"spartial.local/backend/internal/security"
)

type Command struct {
	Actor, Operation, Key, Hash string
	Replay                      []byte
}

func BeginCommand(ctx context.Context, tx pgx.Tx, actor, op, key string, input any) (Command, error) {
	c := Command{Actor: actor, Operation: op, Key: key}
	if len(key) < 8 || len(key) > 128 {
		return c, Fail(422, "idempotency_key_required", "Idempotency-Key must be 8–128 characters")
	}
	body, e := json.Marshal(input)
	if e != nil {
		return c, e
	}
	c.Hash = security.Digest(string(body))
	if e = LockIdempotency(ctx, tx, actor, op, key); e != nil {
		return c, e
	}
	old, replay, e := ReadIdempotency(ctx, tx, actor, op, key)
	if e == nil {
		if old != c.Hash {
			return c, Fail(409, "idempotency_conflict", "Key already used with a different request")
		}
		c.Replay = replay
		return c, nil
	}
	if errors.Is(e, pgx.ErrNoRows) {
		return c, nil
	}
	return c, e
}
func (c Command) Save(ctx context.Context, tx pgx.Tx, response any) error {
	body, e := json.Marshal(response)
	if e != nil {
		return e
	}
	return WriteIdempotency(ctx, tx, c.Actor, c.Operation, c.Key, c.Hash, body)
}
func LockStaffAdministration(ctx context.Context, tx pgx.Tx) error {
	_, e := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(8201771)`)
	return e
}
