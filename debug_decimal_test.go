package main

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	"github.com/ericlagergren/decimal"
	"github.com/hzbay/chain-bridge/internal/config"
	_ "github.com/lib/pq"
	sqltypes "github.com/volatiletech/sqlboiler/v4/types"
)

func TestDecimalEncoding(t *testing.T) {
	// Connect to database
	cfg := config.DefaultServiceConfigFromEnv()
	db, err := sql.Open("postgres", cfg.Database.ConnectionString())
	if err != nil {
		t.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	// Test 1: Try to create a simple decimal
	d := decimal.New(0, 0)
	_, ok := d.SetString("100")
	if !ok {
		t.Fatalf("Failed to create decimal from string")
	}

	sqlDec := sqltypes.NewDecimal(d)

	// Test 2: Try to get the Value() from the decimal
	value, err := sqlDec.Value()
	if err != nil {
		t.Fatalf("Failed to get value from decimal: %v", err)
	}

	fmt.Printf("Decimal value: %v (type: %T)\n", value, value)

	// Test 3: Try direct SQL query with the value
	var result string
	err = db.QueryRowContext(ctx, "SELECT $1::numeric", value).Scan(&result)
	if err != nil {
		t.Fatalf("Failed to query with decimal value: %v", err)
	}

	fmt.Printf("Database result: %s\n", result)

	// Test 4: Try with a prepared statement
	stmt, err := db.PrepareContext(ctx, "SELECT $1::numeric")
	if err != nil {
		t.Fatalf("Failed to prepare statement: %v", err)
	}
	defer stmt.Close()

	err = stmt.QueryRowContext(ctx, sqlDec).Scan(&result)
	if err != nil {
		t.Fatalf("Failed to query with prepared statement: %v", err)
	}

	fmt.Printf("Prepared statement result: %s\n", result)
}
