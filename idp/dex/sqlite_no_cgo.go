//go:build !cgo

package dex

import (
	"fmt"
	"log/slog"

	"github.com/dexidp/dex/storage"
)

func openSQLiteStorage(file string, logger *slog.Logger) (storage.Storage, error) {
	return nil, fmt.Errorf("sqlite3 storage is not available without CGO; recompile with CGO_ENABLED=1 or use postgres storage")
}
