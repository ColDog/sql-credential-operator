package db

import (
	"bytes"
	"context"
	"database/sql"
	"html/template"

	_ "github.com/lib/pq" // Include all sql packages here.
)

var createUserSQL = map[string]string{
	"postgres": `
DO
$do$
BEGIN
	IF NOT EXISTS (
		SELECT
		FROM   pg_catalog.pg_roles
		WHERE  rolname = '{{.Name}}') THEN

		CREATE ROLE {{.Name}} LOGIN PASSWORD '{{.Password}}';
		GRANT {{.Role}} TO {{.Name}};
	END IF;
END
$do$;
	`,
}

var userExistsSQL = map[string]string{
	"postgres": `SELECT 1 FROM pg_stat_activity WHERE usename='{{.Name}}';`,
}

var removeUserSQL = map[string]string{
	"postgres": `DROP USER IF EXISTS {{.Name}};`,
}

// New initializes a new DB. It will return an error if the driver is not registered.
func New(driver, url string) (DB, error) {
	d, err := sql.Open(driver, url)
	if err != nil {
		return nil, err
	}
	return &db{driver: driver, db: d}, nil
}

// User is acted on by a DB.
type User struct {
	Name     string
	Password string
	Role     string
}

// DB represents the interface to execute actions on a DB.
type DB interface {
	Close()
	CreateUser(ctx context.Context, user User) error
	RemoveUser(ctx context.Context, name string) error
	IsActive(ctx context.Context, name string) (bool, error)
}

type db struct {
	driver string
	db     *sql.DB
}

func (db *db) Close() { db.db.Close() }

func (db *db) CreateUser(ctx context.Context, user User) error {
	sql, err := tpl(user, createUserSQL[db.driver])
	if err != nil {
		return err
	}

	_, err = db.db.Exec(sql)
	if err != nil {
		return err
	}
	return nil
}

func (db *db) IsActive(ctx context.Context, name string) (bool, error) {
	sqlStr, err := tpl(map[string]string{"Name": name}, userExistsSQL[db.driver])
	if err != nil {
		return false, err
	}

	var exists *int
	err = db.db.QueryRow(sqlStr).Scan(&exists)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (db *db) RemoveUser(ctx context.Context, name string) error {
	sql, err := tpl(map[string]string{"Name": name}, removeUserSQL[db.driver])
	if err != nil {
		return err
	}

	_, err = db.db.Exec(sql)
	if err != nil {
		return err
	}
	return nil
}

func tpl(m interface{}, tplStr string) (string, error) {
	t, err := template.New("").Parse(tplStr)
	if err != nil {
		return "", err
	}

	buf := bytes.NewBuffer(nil)
	err = t.Execute(buf, m)
	if err != nil {
		return "", err
	}
	return buf.String(), nil
}
