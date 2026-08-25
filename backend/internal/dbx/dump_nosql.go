package dbx

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Dumps for the two engines that are not SQL.
//
// Both have an official tool — mongodump and redis-cli --rdb — and the image
// carries the first. Neither is depended on: mongodump is a separate package
// that a rebuilt image can lose, redis-cli's RDB path needs replication
// permissions the dashboard's login often does not have, and "the backup button
// works on this machine" is not a property worth leaving to what happened to be
// installed. The fallbacks below go through the drivers the dashboard is already
// connected with, so they work wherever the browser tabs do.
//
// The format is gzipped JSON Lines: a metadata line, then one line per document
// or key. It streams in both directions, it survives a truncated file (every
// line before the break is still readable), and it is greppable with zcat.

// dumpArchiveVersion is written into the header. A restore refuses a version it
// does not understand rather than half-reading it.
const dumpArchiveVersion = 1

type dumpArchiveHeader struct {
	Format   string `json:"format"`
	Version  int    `json:"version"`
	Driver   Driver `json:"driver"`
	Database string `json:"database"`
	Taken    string `json:"taken"`
}

// --- Redis ----------------------------------------------------------------

// redisDumpEntry is one key. The payload is Redis's own serialisation — the
// same bytes DUMP produces and RESTORE consumes — which is what makes this
// faithful for every type including the ones with no textual form: a stream's
// entry ids, a sorted set's scores, a hash field's TTL.
//
// Key and payload are base64 because both are binary as far as Redis is
// concerned; a key is a byte string, not text, and one holding invalid UTF-8 is
// legal and would not survive JSON.
type redisDumpEntry struct {
	Key     string `json:"k"`
	Payload string `json:"p"`
	TTLms   int64  `json:"t"`
}

func dumpRedis(ctx context.Context, dsn, database, outDir string) (*DumpResult, error) {
	idx, err := redisDatabaseIndex(dsn, database)
	if err != nil {
		return nil, err
	}
	client, err := RedisClient(ctx, dsn, idx)
	if err != nil {
		return nil, err
	}
	defer client.Close()
	// RedisClient only honours a non-zero index, since zero is also "unset".
	// Selecting explicitly is what makes dumping db0 mean db0 rather than
	// whatever the connection string happened to point at.
	if err := client.Do(ctx, "SELECT", idx).Err(); err != nil {
		return nil, err
	}

	if err := os.MkdirAll(outDir, 0o700); err != nil {
		return nil, err
	}
	start := time.Now()
	path := filepath.Join(outDir, dumpFilename("redis-db"+strconv.Itoa(idx), "redis", "jsonl.gz", start))
	f, gz, w, cleanup, err := createArchive(path, dumpArchiveHeader{
		Format: "jd-redis", Version: dumpArchiveVersion, Driver: DriverRedis,
		Database: strconv.Itoa(idx), Taken: start.UTC().Format(time.RFC3339),
	})
	if err != nil {
		return nil, err
	}
	ok := false
	defer func() { cleanup(&ok) }()

	enc := json.NewEncoder(w)
	var (
		cursor uint64
		keys   int64
	)
	for {
		batch, next, err := client.Scan(ctx, cursor, "*", 500).Result()
		if err != nil {
			return nil, err
		}
		for _, key := range batch {
			payload, err := client.Dump(ctx, key).Result()
			if err == redis.Nil {
				// Expired between the SCAN and the DUMP. Not an error: it is
				// not in the database any more, so it does not belong in a
				// snapshot of the database.
				continue
			}
			if err != nil {
				return nil, fmt.Errorf("cannot dump key %q: %w", key, err)
			}
			ttl, err := client.PTTL(ctx, key).Result()
			if err != nil {
				return nil, err
			}
			ms := int64(0)
			if ttl > 0 {
				ms = ttl.Milliseconds()
			}
			if err := enc.Encode(redisDumpEntry{
				Key:     base64.StdEncoding.EncodeToString([]byte(key)),
				Payload: base64.StdEncoding.EncodeToString([]byte(payload)),
				TTLms:   ms,
			}); err != nil {
				return nil, err
			}
			keys++
		}
		cursor = next
		if cursor == 0 {
			break
		}
	}
	if err := finishArchive(f, gz, w); err != nil {
		return nil, err
	}
	ok = true
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	return &DumpResult{
		Path: path, Size: st.Size(), Driver: DriverRedis, Database: strconv.Itoa(idx),
		Duration:  time.Since(start).Round(time.Millisecond).String(),
		StartedAt: start.UTC(), Summary: fmt.Sprintf("%d keys", keys),
	}, nil
}

func restoreRedis(ctx context.Context, dsn, database, path string) (string, error) {
	idx, err := redisDatabaseIndex(dsn, database)
	if err != nil {
		return "", err
	}
	client, err := RedisClient(ctx, dsn, idx)
	if err != nil {
		return "", err
	}
	defer client.Close()
	if err := client.Do(ctx, "SELECT", idx).Err(); err != nil {
		return "", err
	}
	lines, closeArchive, err := openArchive(path, "jd-redis")
	if err != nil {
		return "", err
	}
	defer closeArchive()

	var restored int64
	for lines.Scan() {
		var e redisDumpEntry
		if err := json.Unmarshal(lines.Bytes(), &e); err != nil {
			return "", fmt.Errorf("corrupt dump at key %d: %w", restored+1, err)
		}
		key, err := base64.StdEncoding.DecodeString(e.Key)
		if err != nil {
			return "", err
		}
		payload, err := base64.StdEncoding.DecodeString(e.Payload)
		if err != nil {
			return "", err
		}
		// REPLACE, because a restore is a restore: without it every key that
		// already exists fails with BUSYKEY and the operator gets a half-loaded
		// database and a wall of errors.
		if err := client.RestoreReplace(ctx, string(key), time.Duration(e.TTLms)*time.Millisecond, string(payload)).Err(); err != nil {
			return "", fmt.Errorf("cannot restore key %q: %w", key, err)
		}
		restored++
	}
	if err := lines.Err(); err != nil {
		return "", err
	}
	return fmt.Sprintf("%d keys restored into db%d", restored, idx), nil
}

// redisDatabaseIndex resolves which numbered database to act on. Redis names
// them with integers, so the "database" the rest of the product passes around
// as a string is one here — and an empty one means the connection's own, which
// is what the operator sees when they open the browser.
func redisDatabaseIndex(dsn, database string) (int, error) {
	if strings.TrimSpace(database) != "" {
		n, err := strconv.Atoi(strings.TrimSpace(database))
		if err != nil || n < 0 {
			return 0, fmt.Errorf("redis databases are numbered; %q is not a number", database)
		}
		return n, nil
	}
	if opt, err := redis.ParseURL(dsn); err == nil {
		return opt.DB, nil
	}
	return 0, nil
}

// --- Mongo ----------------------------------------------------------------

type mongoDumpEntry struct {
	Collection string          `json:"c,omitempty"`
	Document   json.RawMessage `json:"d,omitempty"`
}

// dumpMongoDriver is the fallback for a machine with no mongodump. Documents go
// out as canonical Extended JSON, which is the format that survives the types
// BSON has and JSON does not — an ObjectId stays an ObjectId, a 64-bit integer
// does not become a float, a date does not become a string.
func dumpMongoDriver(ctx context.Context, dsn, database, outDir string) (*DumpResult, error) {
	client, err := MongoClient(ctx, dsn)
	if err != nil {
		return nil, err
	}
	defer client.Disconnect(context.Background())

	if database == "" {
		return nil, fmt.Errorf("no database named in the connection string; specify one explicitly")
	}
	if err := os.MkdirAll(outDir, 0o700); err != nil {
		return nil, err
	}
	start := time.Now()
	path := filepath.Join(outDir, dumpFilename(database, "mongo", "jsonl.gz", start))
	f, gz, w, cleanup, err := createArchive(path, dumpArchiveHeader{
		Format: "jd-mongo", Version: dumpArchiveVersion, Driver: DriverMongo,
		Database: database, Taken: start.UTC().Format(time.RFC3339),
	})
	if err != nil {
		return nil, err
	}
	ok := false
	defer func() { cleanup(&ok) }()

	db := client.Database(database)
	names, err := db.ListCollectionNames(ctx, bson.D{})
	if err != nil {
		return nil, err
	}
	enc := json.NewEncoder(w)
	var docs int64
	for _, name := range names {
		if strings.HasPrefix(name, "system.") {
			continue
		}
		// The collection line comes first even when the collection is empty, so
		// a restore recreates it rather than silently dropping it.
		if err := enc.Encode(mongoDumpEntry{Collection: name}); err != nil {
			return nil, err
		}
		cur, err := db.Collection(name).Find(ctx, bson.D{}, options.Find().SetBatchSize(500))
		if err != nil {
			return nil, err
		}
		for cur.Next(ctx) {
			ext, err := bson.MarshalExtJSON(cur.Current, true, false)
			if err != nil {
				cur.Close(ctx)
				return nil, err
			}
			if err := enc.Encode(mongoDumpEntry{Document: ext}); err != nil {
				cur.Close(ctx)
				return nil, err
			}
			docs++
		}
		err = cur.Err()
		cur.Close(ctx)
		if err != nil {
			return nil, err
		}
	}
	if err := finishArchive(f, gz, w); err != nil {
		return nil, err
	}
	ok = true
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	return &DumpResult{
		Path: path, Size: st.Size(), Driver: DriverMongo, Database: database,
		Duration:  time.Since(start).Round(time.Millisecond).String(),
		StartedAt: start.UTC(),
		Summary:   fmt.Sprintf("%d collections, %d documents", len(names), docs),
	}, nil
}

func restoreMongoDriver(ctx context.Context, dsn, database, path string) (string, error) {
	client, err := MongoClient(ctx, dsn)
	if err != nil {
		return "", err
	}
	defer client.Disconnect(context.Background())
	lines, closeArchive, err := openArchive(path, "jd-mongo")
	if err != nil {
		return "", err
	}
	defer closeArchive()

	db := client.Database(database)
	var (
		current *mongo.Collection
		batch   []any
		docs    int64
		colls   int
	)
	flush := func() error {
		if current == nil || len(batch) == 0 {
			return nil
		}
		_, err := current.InsertMany(ctx, batch)
		batch = batch[:0]
		return err
	}
	for lines.Scan() {
		var e mongoDumpEntry
		if err := json.Unmarshal(lines.Bytes(), &e); err != nil {
			return "", fmt.Errorf("corrupt dump near document %d: %w", docs+1, err)
		}
		if e.Collection != "" {
			if err := flush(); err != nil {
				return "", err
			}
			// Dropping first is what makes the restore a restore rather than a
			// merge: without it the _id values in the dump collide with the
			// ones already there and every insert fails.
			if err := db.Collection(e.Collection).Drop(ctx); err != nil {
				return "", err
			}
			if err := db.CreateCollection(ctx, e.Collection); err != nil &&
				!strings.Contains(err.Error(), "already exists") {
				return "", err
			}
			current = db.Collection(e.Collection)
			colls++
			continue
		}
		if current == nil {
			return "", fmt.Errorf("dump starts with a document before naming a collection")
		}
		var doc bson.D
		if err := bson.UnmarshalExtJSON(e.Document, true, &doc); err != nil {
			return "", fmt.Errorf("corrupt document %d: %w", docs+1, err)
		}
		batch = append(batch, doc)
		docs++
		if len(batch) >= 500 {
			if err := flush(); err != nil {
				return "", err
			}
		}
	}
	if err := lines.Err(); err != nil {
		return "", err
	}
	if err := flush(); err != nil {
		return "", err
	}
	return fmt.Sprintf("%d collections, %d documents restored into %s", colls, docs, database), nil
}

// --- archive plumbing -----------------------------------------------------

// createArchive opens the file, wraps it in gzip and a buffer, and writes the
// header line. The returned cleanup removes the file unless the caller flips
// its flag, for the same reason the SQL dump does: a partial file that looks
// like a backup is the failure mode worth engineering against.
func createArchive(path string, header dumpArchiveHeader) (*os.File, *gzip.Writer, *bufio.Writer, func(*bool), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	gz := gzip.NewWriter(f)
	w := bufio.NewWriterSize(gz, 64<<10)
	cleanup := func(ok *bool) {
		f.Close()
		if !*ok {
			os.Remove(path)
		}
	}
	if err := json.NewEncoder(w).Encode(header); err != nil {
		ok := false
		cleanup(&ok)
		return nil, nil, nil, nil, err
	}
	return f, gz, w, cleanup, nil
}

func finishArchive(f *os.File, gz *gzip.Writer, w *bufio.Writer) error {
	if err := w.Flush(); err != nil {
		return err
	}
	if err := gz.Close(); err != nil {
		return err
	}
	return f.Sync()
}

// openArchive reads the header, checks it is the format the caller expects, and
// returns a scanner positioned on the first entry.
func openArchive(path, format string) (*bufio.Scanner, func(), error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	gz, err := gzip.NewReader(f)
	if err != nil {
		f.Close()
		return nil, nil, fmt.Errorf("%s is not a Just Dashboard archive: %w", filepath.Base(path), err)
	}
	closeAll := func() { gz.Close(); f.Close() }
	sc := bufio.NewScanner(gz)
	// A single document can be 16 MB in Mongo and a Redis value larger still,
	// so the default 64 KB line cap would refuse to read back what this wrote.
	sc.Buffer(make([]byte, 0, 256<<10), 64<<20)
	if !sc.Scan() {
		closeAll()
		return nil, nil, fmt.Errorf("archive is empty")
	}
	var h dumpArchiveHeader
	if err := json.Unmarshal(sc.Bytes(), &h); err != nil {
		closeAll()
		return nil, nil, fmt.Errorf("archive has no header: %w", err)
	}
	if h.Format != format {
		closeAll()
		return nil, nil, fmt.Errorf("this is a %s archive, not %s", h.Format, format)
	}
	if h.Version > dumpArchiveVersion {
		closeAll()
		return nil, nil, fmt.Errorf("archive was written by a newer version of the dashboard")
	}
	return sc, closeAll, nil
}
