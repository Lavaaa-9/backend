package integration

import (
	"context"
	"database/sql"
	"encoding/json"
	"github.com/pressly/goose/v3"
	"os"
	"spartial.local/backend/internal/platform"
	"testing"
)

func TestSecurityMigrationsDownAndUp(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	conn, e := sql.Open("pgx", f.owner.Config().ConnConfig.ConnString())
	if e != nil {
		t.Fatal(e)
	}
	defer conn.Close()
	if e = goose.DownToContext(ctx, conn, "migrations", 3); e != nil {
		t.Fatal(e)
	}
	if e = goose.UpContext(ctx, conn, "migrations"); e != nil {
		t.Fatal(e)
	}
	if e = platform.GrantRuntime(ctx, f.owner, f.runtime.Config().ConnConfig.User); e != nil {
		t.Fatal(e)
	}
	if e = platform.CheckRuntimeRole(ctx, f.runtime); e != nil {
		t.Fatal(e)
	}
	if e = platform.CheckSchema(ctx, f.runtime); e != nil {
		t.Fatal(e)
	}
}

func TestEverySchemaColumnHasClassification(t *testing.T) {
	f := setup(t)
	data, e := os.ReadFile("../../deploy/data-classification.json")
	if e != nil {
		t.Fatal(e)
	}
	catalog := map[string]map[string]string{}
	if e = json.Unmarshal(data, &catalog); e != nil {
		t.Fatal(e)
	}
	rows, e := f.owner.Query(context.Background(), `SELECT table_name,column_name FROM information_schema.columns WHERE table_schema='public'`)
	if e != nil {
		t.Fatal(e)
	}
	defer rows.Close()
	for rows.Next() {
		var table, column string
		if e = rows.Scan(&table, &column); e != nil {
			t.Fatal(e)
		}
		switch catalog[table][column] {
		case "C1", "C2", "C3", "C4":
		default:
			t.Fatalf("unclassified schema column %s.%s", table, column)
		}
	}
	if e = rows.Err(); e != nil {
		t.Fatal(e)
	}
}
