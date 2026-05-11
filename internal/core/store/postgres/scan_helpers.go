package postgres

type rowScanner interface {
	Scan(dest ...any) error
}
