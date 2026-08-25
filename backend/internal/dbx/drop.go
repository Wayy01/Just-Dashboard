package dbx

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/mongo"
)

// Removing a database, as opposed to forgetting a connection to one.
//
// The dashboard could create a database and connect to it, and the only thing
// it could then do with one was stop listing it — "Remove" took the connection
// out of the sidebar and left the database on disk. A panel that can bring
// something into existence and cannot take it out again is one that quietly
// accumulates the experiments you made with it.
//
// Every engine spells this differently, and two of them do not have the verb at
// all: a SQLite database is a file, and a Redis "database" is a numbered
// keyspace that exists whether or not anything is in it. Those are handled as
// what they are rather than pretended into a DROP.

// DropResult says what actually happened, because the same button means three
// different things across the eight engines and the operator should be told
// which one they got.
type DropResult struct {
	// Detail is one line for the toast: "database dropped", "file removed",
	// "keyspace flushed".
	Detail string `json:"detail"`
	// Gone reports whether the thing named no longer exists at all. Redis is
	// the one engine where it is false on success: db0 is still there, empty.
	Gone bool `json:"gone"`
}

// DropDatabase removes a database entirely. The caller is responsible for the
// typed confirmation and for closing any pool it holds on this connection —
// Postgres refuses to drop a database anything is connected to, and the
// dashboard's own pool is something connected to it.
func DropDatabase(ctx context.Context, driver Driver, dsn, database string) (*DropResult, error) {
	switch driver {
	case DriverSQLite:
		return dropSQLiteFile(dsn)
	case DriverRedis:
		return flushRedisDatabase(ctx, dsn, database)
	case DriverMongo:
		return dropMongoDatabase(ctx, dsn, database)
	}
	return dropSQLDatabase(ctx, driver, dsn, database)
}

func dropSQLDatabase(ctx context.Context, driver Driver, dsn, database string) (*DropResult, error) {
	d, err := DialectFor(driver)
	if err != nil {
		return nil, err
	}
	if err := validateDumpDatabase(database); err != nil {
		return nil, err
	}
	stmts, err := d.DropDatabaseSQL(database)
	if err != nil {
		return nil, err
	}
	// Move off the database being dropped where the engine insists on it. The
	// admin database is one every install of that engine has, so this cannot
	// fail for want of somewhere to go.
	target := dsn
	if admin := d.AdminDatabase(); admin != "" && !strings.EqualFold(admin, database) {
		target = dsnForDatabase(driver, dsn, admin)
	}
	db, err := openForDump(ctx, d, target)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	for i, stmt := range stmts {
		_, err := db.ExecContext(ctx, stmt.SQL, stmt.Args...)
		if err == nil {
			continue
		}
		// Only the last statement has to work; see Dialect.DropDatabaseSQL.
		if i < len(stmts)-1 {
			continue
		}
		return nil, err
	}
	return &DropResult{Detail: "database dropped", Gone: true}, nil
}

// dropSQLiteFile removes the database file and the two files SQLite keeps
// beside it. Leaving the -wal behind is not harmless: a later connection to a
// recreated database of the same name would replay it.
func dropSQLiteFile(dsn string) (*DropResult, error) {
	info, err := ParseDSN(DriverSQLite, dsn)
	if err != nil {
		return nil, err
	}
	if info.Database == "" {
		return nil, fmt.Errorf("connection has no database file path")
	}
	if err := os.Remove(info.Database); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &DropResult{Detail: "file was already gone", Gone: true}, nil
		}
		return nil, err
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		_ = os.Remove(info.Database + suffix)
	}
	return &DropResult{Detail: "file removed", Gone: true}, nil
}

// flushRedisDatabase empties one numbered keyspace.
//
// Redis has no DROP: the sixteen databases are configured at startup and exist
// whether or not anything is in them, so the honest equivalent of "delete this
// database" is FLUSHDB, and the result says so rather than claiming the
// database is gone.
func flushRedisDatabase(ctx context.Context, dsn, database string) (*DropResult, error) {
	idx, err := redisDatabaseIndex(dsn, strings.TrimPrefix(strings.TrimSpace(database), "db"))
	if err != nil {
		return nil, err
	}
	client, err := RedisClient(ctx, dsn, idx)
	if err != nil {
		return nil, err
	}
	defer client.Close()
	if err := client.Do(ctx, "SELECT", idx).Err(); err != nil {
		return nil, err
	}
	n, err := client.DBSize(ctx).Result()
	if err != nil {
		return nil, err
	}
	if err := client.FlushDB(ctx).Err(); err != nil {
		return nil, err
	}
	return &DropResult{
		Detail: fmt.Sprintf("db%d emptied — %d keys deleted; Redis keyspaces cannot be removed, only cleared", idx, n),
		Gone:   false,
	}, nil
}

func dropMongoDatabase(ctx context.Context, dsn, database string) (*DropResult, error) {
	if err := validateDumpDatabase(database); err != nil {
		return nil, err
	}
	client, err := MongoClient(ctx, dsn)
	if err != nil {
		return nil, err
	}
	defer func() {
		disconnect, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = client.Disconnect(disconnect)
	}()
	if err := client.Database(database).Drop(ctx); err != nil {
		var cmdErr mongo.CommandError
		if errors.As(err, &cmdErr) {
			return nil, fmt.Errorf("%s", cmdErr.Message)
		}
		return nil, err
	}
	return &DropResult{Detail: "database dropped", Gone: true}, nil
}
