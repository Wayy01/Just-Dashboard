package dbx

import (
	"context"
	"fmt"
	"sort"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// MongoClient is opened per request rather than pooled like the SQL drivers.
// The Mongo driver already multiplexes internally, and connections here are
// short-lived administrative reads.
func MongoClient(ctx context.Context, uri string) (*mongo.Client, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri).SetServerSelectionTimeout(8*time.Second))
	if err != nil {
		return nil, err
	}
	if err := client.Ping(ctx, nil); err != nil {
		client.Disconnect(context.Background())
		return nil, err
	}
	return client, nil
}

func MongoDatabases(ctx context.Context, client *mongo.Client) ([]Database, error) {
	res, err := client.ListDatabases(ctx, bson.D{})
	if err != nil {
		return nil, err
	}
	out := make([]Database, 0, len(res.Databases))
	for _, d := range res.Databases {
		out = append(out, Database{Name: d.Name, Size: d.SizeOnDisk})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// MongoCollections reports collections as "tables" so the frontend can render
// one browser for every engine instead of a separate Mongo-specific view.
func MongoCollections(ctx context.Context, client *mongo.Client, dbName string) ([]Table, error) {
	db := client.Database(dbName)
	names, err := db.ListCollectionNames(ctx, bson.D{})
	if err != nil {
		return nil, err
	}
	out := make([]Table, 0, len(names))
	for _, name := range names {
		t := Table{Schema: dbName, Name: name, Type: "collection"}
		var stats struct {
			Count int64 `bson:"count"`
			Size  int64 `bson:"size"`
		}
		if err := db.RunCommand(ctx, bson.D{{Key: "collStats", Value: name}}).Decode(&stats); err == nil {
			t.Rows, t.Size = stats.Count, stats.Size
		}
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// MongoFind runs a filter against a collection. The filter arrives as JSON and
// is parsed as extended JSON, which is what Compass and the shell emit.
func MongoFind(ctx context.Context, client *mongo.Client, dbName, collection, filterJSON string, limit, skip int) (*QueryResult, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	filter := bson.D{}
	if filterJSON != "" && filterJSON != "{}" {
		if err := bson.UnmarshalExtJSON([]byte(filterJSON), false, &filter); err != nil {
			return nil, fmt.Errorf("filter is not valid JSON: %w", err)
		}
	}
	start := time.Now()
	cur, err := client.Database(dbName).Collection(collection).Find(ctx, filter,
		options.Find().SetLimit(int64(limit)).SetSkip(int64(skip)))
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)

	// Documents are heterogeneous, so the column set is the union of the keys
	// actually seen — otherwise a sparse collection renders as one column.
	docs := []map[string]any{}
	columns := []string{}
	seen := map[string]bool{}
	for cur.Next(ctx) {
		var doc bson.M
		if err := cur.Decode(&doc); err != nil {
			return nil, err
		}
		flat := map[string]any{}
		for k, v := range doc {
			flat[k] = normaliseBSON(v)
			if !seen[k] {
				seen[k] = true
				columns = append(columns, k)
			}
		}
		docs = append(docs, flat)
	}
	if err := cur.Err(); err != nil {
		return nil, err
	}
	sort.Slice(columns, func(i, j int) bool {
		// _id first, then alphabetical: the identifier is what an operator
		// scans for.
		if columns[i] == "_id" {
			return true
		}
		if columns[j] == "_id" {
			return false
		}
		return columns[i] < columns[j]
	})
	res := &QueryResult{
		Columns: columns, Types: []string{}, Rows: [][]any{},
		Statement: fmt.Sprintf("db.%s.find(%s)", collection, defaultFilter(filterJSON)),
	}
	for _, doc := range docs {
		row := make([]any, len(columns))
		for i, c := range columns {
			row[i] = doc[c]
		}
		res.Rows = append(res.Rows, row)
	}
	res.RowCount = len(res.Rows)
	res.Truncated = res.RowCount >= limit
	res.Duration = time.Since(start).Round(time.Microsecond).String()
	return res, nil
}

func defaultFilter(f string) string {
	if f == "" {
		return "{}"
	}
	return f
}

// normaliseBSON renders BSON-specific types as strings so the JSON response is
// readable rather than a nest of {"$oid": …} wrappers.
func normaliseBSON(v any) any {
	switch t := v.(type) {
	case bson.M:
		out := map[string]any{}
		for k, inner := range t {
			out[k] = normaliseBSON(inner)
		}
		return out
	case bson.A:
		out := make([]any, 0, len(t))
		for _, inner := range t {
			out = append(out, normaliseBSON(inner))
		}
		return out
	case primitiveStringer:
		return t.String()
	case time.Time:
		return t.UTC().Format(time.RFC3339Nano)
	default:
		return v
	}
}

type primitiveStringer interface{ String() string }

func MongoServerStatus(ctx context.Context, client *mongo.Client) (map[string]any, error) {
	var raw bson.M
	err := client.Database("admin").RunCommand(ctx, bson.D{{Key: "serverStatus", Value: 1}}).Decode(&raw)
	if err != nil {
		return nil, err
	}
	out := map[string]any{}
	// Only the fields an operator watches are surfaced; serverStatus is
	// hundreds of keys deep and mostly irrelevant here.
	for _, key := range []string{"host", "version", "uptime", "connections", "network", "opcounters", "mem"} {
		if v, ok := raw[key]; ok {
			out[key] = normaliseBSON(v)
		}
	}
	return out, nil
}
