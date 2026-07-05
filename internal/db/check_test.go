package db

import (
	"path/filepath"
	"testing"
)

func TestCheckSchema(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "schema.db")
	if err := Init(dbPath); err != nil {
		t.Fatalf("Init() error=%v", err)
	}
	t.Cleanup(func() { DB = nil })

	var m []map[string]interface{}
	if err := DB.Raw("PRAGMA table_info(devices)").Scan(&m).Error; err != nil {
		t.Fatalf("schema query error=%v", err)
	}
	if len(m) == 0 {
		t.Fatal("devices schema is empty")
	}
}
