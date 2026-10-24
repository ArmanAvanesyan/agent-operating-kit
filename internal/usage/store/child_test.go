package store

import "database/sql"

func sqlOpenChild(path string) (*sql.DB, error) { return sql.Open("sqlite", path) }
