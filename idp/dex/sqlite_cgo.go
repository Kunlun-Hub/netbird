//go:build cgo

package dex

import (
	"log/slog"

	"github.com/dexidp/dex/storage"
	"github.com/dexidp/dex/storage/sql"
)

func openSQLiteStorage(file string, logger *slog.Logger) (storage.Storage, error) {
	return (&sql.SQLite3{File: file}).Open(logger)
}
