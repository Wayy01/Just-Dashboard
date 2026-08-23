package dbx

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
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

// MongoFind runs a filter against a collection, optionally sorted and
// projected. The filter and sort arrive as JSON and are parsed as extended
// JSON, which is what Compass and the shell emit.
func MongoFind(ctx context.Context, client *mongo.Client, dbName, collection, filterJSON string, limit, skip int) (*QueryResult, error) {
	return MongoQuery(ctx, client, dbName, collection, MongoFindOptions{
		Filter: filterJSON, Limit: limit, Skip: skip,
	})
}

// MongoFindOptions is the document-store equivalent of BrowseOptions.
type MongoFindOptions struct {
	Filter     string
	Sort       string
	Projection string
	Limit      int
	Skip       int
}

func MongoQuery(ctx context.Context, client *mongo.Client, dbName, collection string, opts MongoFindOptions) (*QueryResult, error) {
	if opts.Limit <= 0 || opts.Limit > 1000 {
		opts.Limit = 100
	}
	filter, err := mongoParseFilter(opts.Filter)
	if err != nil {
		return nil, err
	}
	find := options.Find().SetLimit(int64(opts.Limit)).SetSkip(int64(opts.Skip))
	if strings.TrimSpace(opts.Sort) != "" && strings.TrimSpace(opts.Sort) != "{}" {
		sortDoc := bson.D{}
		if err := bson.UnmarshalExtJSON([]byte(opts.Sort), false, &sortDoc); err != nil {
			return nil, fmt.Errorf("sort is not valid JSON: %w", err)
		}
		find.SetSort(sortDoc)
	}
	if strings.TrimSpace(opts.Projection) != "" && strings.TrimSpace(opts.Projection) != "{}" {
		projDoc := bson.D{}
		if err := bson.UnmarshalExtJSON([]byte(opts.Projection), false, &projDoc); err != nil {
			return nil, fmt.Errorf("projection is not valid JSON: %w", err)
		}
		find.SetProjection(projDoc)
	}
	start := time.Now()
	cur, err := client.Database(dbName).Collection(collection).Find(ctx, filter, find)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)

	res := docsToResult(ctx, cur, opts.Limit,
		fmt.Sprintf("db.%s.find(%s)", collection, defaultFilter(opts.Filter)))
	res.Duration = time.Since(start).Round(time.Microsecond).String()
	return res, nil
}

// docsToResult flattens a cursor of heterogeneous documents into the tabular
// shape the grid renders.
//
// The column set is the union of the keys actually seen rather than the first
// document's keys: a collection where later documents carry fields the first
// one lacks would otherwise render those fields nowhere, which is the normal
// state of an evolving schema rather than an edge case.
func docsToResult(ctx context.Context, cur *mongo.Cursor, limit int, statement string) *QueryResult {
	docs := []map[string]any{}
	columns := []string{}
	seen := map[string]bool{}
	for cur.Next(ctx) {
		if len(docs) >= limit {
			break
		}
		var doc bson.M
		if err := cur.Decode(&doc); err != nil {
			continue
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
	sort.Slice(columns, func(i, j int) bool {
		// _id first, then alphabetical: the identifier is what an operator
		// scans for, and it is also what an edit is keyed on.
		if columns[i] == "_id" {
			return true
		}
		if columns[j] == "_id" {
			return false
		}
		return columns[i] < columns[j]
	})
	res := &QueryResult{
		Columns: columns, Types: []string{}, Rows: [][]any{}, Statement: statement,
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
	return res
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
	case primitive.ObjectID:
		// Bare hex, not ObjectID("…"): this value is what the grid shows and
		// what an edit sends back as the filter, so it has to round-trip.
		return t.Hex()
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

// --- Mongo write and analysis surface ------------------------------------
//
// Everything below brings Mongo up to what the SQL engines can do. The shapes
// differ — a filter document instead of a WHERE clause, a pipeline instead of a
// query — but the guarantees are the same ones the SQL path makes: a write is
// scoped by an explicit filter the caller supplied, never by a filter this code
// guessed at, and a document edit that cannot identify exactly what it is
// changing is refused rather than run.

// mongoParseFilter turns a filter from the client into a BSON document.
//
// It accepts extended JSON, which is what the shell and Compass emit, and adds
// one convenience the raw parser does not: an `_id` given as a bare 24-character
// hex string becomes an ObjectID. That string is exactly what the data grid
// displays, so "edit the row I am looking at" works without the operator having
// to know to wrap it in {"$oid": …} — and a value that is not a valid ObjectID
// is left as the string it is, because plenty of collections use string ids.
func mongoParseFilter(filterJSON string) (bson.D, error) {
	filter := bson.D{}
	if strings.TrimSpace(filterJSON) == "" || strings.TrimSpace(filterJSON) == "{}" {
		return filter, nil
	}
	if err := bson.UnmarshalExtJSON([]byte(filterJSON), false, &filter); err != nil {
		return nil, fmt.Errorf("filter is not valid JSON: %w", err)
	}
	for i, e := range filter {
		if e.Key != "_id" {
			continue
		}
		if s, ok := e.Value.(string); ok {
			if oid, err := primitive.ObjectIDFromHex(s); err == nil {
				filter[i].Value = oid
			}
		}
	}
	return filter, nil
}

func mongoParseDoc(docJSON string) (bson.D, error) {
	doc := bson.D{}
	if strings.TrimSpace(docJSON) == "" {
		return nil, fmt.Errorf("a document is required")
	}
	if err := bson.UnmarshalExtJSON([]byte(docJSON), false, &doc); err != nil {
		return nil, fmt.Errorf("document is not valid JSON: %w", err)
	}
	return doc, nil
}

// MongoInsert adds one document and reports the id it was stored under.
func MongoInsert(ctx context.Context, client *mongo.Client, dbName, collection, docJSON string) (string, error) {
	doc, err := mongoParseDoc(docJSON)
	if err != nil {
		return "", err
	}
	res, err := client.Database(dbName).Collection(collection).InsertOne(ctx, doc)
	if err != nil {
		return "", err
	}
	// Bare hex, matching what the grid displays and what an edit sends back as
	// its filter. fmt.Sprint on an ObjectID yields `ObjectID("…")`, which is not
	// a value anything downstream can use — pasting it into a filter is not even
	// valid JSON.
	if oid, ok := res.InsertedID.(primitive.ObjectID); ok {
		return oid.Hex(), nil
	}
	return fmt.Sprint(res.InsertedID), nil
}

// MongoReplace replaces the single document matching filter.
//
// ReplaceOne, not UpdateMany: the editor shows one document and the write must
// touch exactly that one. An empty filter is refused for the same reason a row
// UPDATE with no primary key is — it would silently rewrite the first document
// in the collection, which is never what editing a record meant.
func MongoReplace(ctx context.Context, client *mongo.Client, dbName, collection, filterJSON, docJSON string) (int64, error) {
	filter, err := mongoParseFilter(filterJSON)
	if err != nil {
		return 0, err
	}
	if len(filter) == 0 {
		return 0, fmt.Errorf("a filter identifying the document is required; an empty filter would rewrite an arbitrary document")
	}
	doc, err := mongoParseDoc(docJSON)
	if err != nil {
		return 0, err
	}
	// _id is immutable in Mongo, and a replacement carrying the same _id is
	// accepted while one carrying a different _id is an error. Dropping it from
	// the replacement entirely sidesteps both cases: the filter already says
	// which document this is.
	clean := make(bson.D, 0, len(doc))
	for _, e := range doc {
		if e.Key != "_id" {
			clean = append(clean, e)
		}
	}
	res, err := client.Database(dbName).Collection(collection).ReplaceOne(ctx, filter, clean)
	if err != nil {
		return 0, err
	}
	return res.ModifiedCount, nil
}

// MongoDelete removes the documents matching filter. An empty filter is refused
// here too: "delete everything in this collection" is a different, louder
// action than deleting a document, and it goes through DropCollection.
func MongoDelete(ctx context.Context, client *mongo.Client, dbName, collection, filterJSON string, many bool) (int64, error) {
	filter, err := mongoParseFilter(filterJSON)
	if err != nil {
		return 0, err
	}
	if len(filter) == 0 {
		return 0, fmt.Errorf("a filter is required; use Drop collection to empty a collection")
	}
	coll := client.Database(dbName).Collection(collection)
	if many {
		res, err := coll.DeleteMany(ctx, filter)
		if err != nil {
			return 0, err
		}
		return res.DeletedCount, nil
	}
	res, err := coll.DeleteOne(ctx, filter)
	if err != nil {
		return 0, err
	}
	return res.DeletedCount, nil
}

// MongoCount reports how many documents match a filter.
func MongoCount(ctx context.Context, client *mongo.Client, dbName, collection, filterJSON string) (int64, error) {
	filter, err := mongoParseFilter(filterJSON)
	if err != nil {
		return 0, err
	}
	return client.Database(dbName).Collection(collection).CountDocuments(ctx, filter)
}

// MongoIndexes lists a collection's indexes in the same shape the SQL engines
// report theirs, so the Structure tab renders one table for every engine.
func MongoIndexes(ctx context.Context, client *mongo.Client, dbName, collection string) ([]Index, error) {
	cur, err := client.Database(dbName).Collection(collection).Indexes().List(ctx)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	out := []Index{}
	for cur.Next(ctx) {
		var spec struct {
			Name   string `bson:"name"`
			Key    bson.D `bson:"key"`
			Unique bool   `bson:"unique"`
		}
		if err := cur.Decode(&spec); err != nil {
			return nil, err
		}
		ix := Index{Name: spec.Name, Unique: spec.Unique, Columns: []string{}}
		for _, k := range spec.Key {
			// The value is the direction (1/-1) or an index type; showing it
			// next to the field is what distinguishes an ascending index from a
			// descending or text one at a glance.
			ix.Columns = append(ix.Columns, fmt.Sprintf("%s:%v", k.Key, k.Value))
		}
		// Mongo's _id index is the closest thing it has to a primary key, and
		// labelling it so lets the row editor treat _id the way it treats one.
		ix.Primary = spec.Name == "_id_"
		out = append(out, ix)
	}
	return out, cur.Err()
}

// MongoAggregate runs a pipeline and renders the result like any other result
// set. This is Mongo's equivalent of the Query tab, and it is read-mostly by
// construction — but not entirely, so the handler classifies the pipeline for
// the writing stages before letting it run.
func MongoAggregate(ctx context.Context, client *mongo.Client, dbName, collection, pipelineJSON string, limit int) (*QueryResult, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	pipeline := []bson.D{}
	if err := bson.UnmarshalExtJSON([]byte("{\"p\":"+pipelineJSON+"}"), false, &struct {
		P *[]bson.D `bson:"p"`
	}{P: &pipeline}); err != nil {
		return nil, fmt.Errorf("pipeline is not a valid JSON array of stages: %w", err)
	}
	start := time.Now()
	cur, err := client.Database(dbName).Collection(collection).Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	res := docsToResult(ctx, cur, limit, fmt.Sprintf("db.%s.aggregate(%s)", collection, pipelineJSON))
	res.Duration = time.Since(start).Round(time.Microsecond).String()
	return res, nil
}

// MongoWritesInPipeline reports whether an aggregation ends in a stage that
// writes. $out and $merge replace or update a whole collection, so a pipeline
// carrying either is treated as destructive rather than as a read.
func MongoWritesInPipeline(pipelineJSON string) bool {
	lower := strings.ToLower(pipelineJSON)
	return strings.Contains(lower, `"$out"`) || strings.Contains(lower, `"$merge"`)
}

// MongoCreateCollection makes an empty collection, which Mongo otherwise only
// creates implicitly on first write — leaving no way to set one up in advance.
func MongoCreateCollection(ctx context.Context, client *mongo.Client, dbName, collection string) error {
	return client.Database(dbName).CreateCollection(ctx, collection)
}

// MongoDropCollection removes a collection and everything in it.
func MongoDropCollection(ctx context.Context, client *mongo.Client, dbName, collection string) error {
	return client.Database(dbName).Collection(collection).Drop(ctx)
}

// MongoCollStats reports the per-collection figures the Structure tab shows.
func MongoCollStats(ctx context.Context, client *mongo.Client, dbName, collection string) (map[string]any, error) {
	var raw bson.M
	err := client.Database(dbName).RunCommand(ctx, bson.D{{Key: "collStats", Value: collection}}).Decode(&raw)
	if err != nil {
		return nil, err
	}
	out := map[string]any{}
	for _, key := range []string{"count", "size", "avgObjSize", "storageSize", "totalIndexSize", "nindexes", "capped"} {
		if v, ok := raw[key]; ok {
			out[key] = normaliseBSON(v)
		}
	}
	return out, nil
}

// --- Mongo export and import ---------------------------------------------

// MongoExport streams a filtered find straight to w in CSV or JSON.
//
// It cannot share StreamExport, which walks a *sql.Rows: the two engines differ
// in exactly the way that matters here. A SQL result set has a fixed column
// list known before the first row; a Mongo cursor does not, because documents
// are heterogeneous. CSV needs a header before any row is written, so the
// column set has to be settled up front — this makes a bounded first pass to
// learn the keys, then a second to write. JSON has no such constraint and
// streams in one pass.
func MongoExport(
	ctx context.Context,
	client *mongo.Client,
	dbName, collection string,
	opts MongoFindOptions,
	format ExportFormat,
	w io.Writer,
	maxRows int,
) (int, bool, error) {
	if maxRows <= 0 || maxRows > 1_000_000 {
		maxRows = 100_000
	}
	filter, err := mongoParseFilter(opts.Filter)
	if err != nil {
		return 0, false, err
	}
	find := options.Find().SetLimit(int64(maxRows))
	if strings.TrimSpace(opts.Sort) != "" && strings.TrimSpace(opts.Sort) != "{}" {
		sortDoc := bson.D{}
		if err := bson.UnmarshalExtJSON([]byte(opts.Sort), false, &sortDoc); err != nil {
			return 0, false, fmt.Errorf("sort is not valid JSON: %w", err)
		}
		find.SetSort(sortDoc)
	}

	coll := client.Database(dbName).Collection(collection)

	if format == ExportJSON {
		cur, err := coll.Find(ctx, filter, find)
		if err != nil {
			return 0, false, err
		}
		defer cur.Close(ctx)
		return mongoStreamJSON(ctx, cur, w, maxRows)
	}

	// CSV: settle the header first.
	columns, err := mongoColumnUnion(ctx, coll, filter, find, maxRows)
	if err != nil {
		return 0, false, err
	}
	cur, err := coll.Find(ctx, filter, find)
	if err != nil {
		return 0, false, err
	}
	defer cur.Close(ctx)
	return mongoStreamCSV(ctx, cur, columns, w, maxRows)
}

// mongoColumnUnion reads the documents once to learn every key present, so the
// CSV header covers fields that only later documents carry. The scan is bounded
// by the same row cap as the export itself.
func mongoColumnUnion(ctx context.Context, coll *mongo.Collection, filter any, find *options.FindOptions, maxRows int) ([]string, error) {
	cur, err := coll.Find(ctx, filter, find)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	seen := map[string]bool{}
	columns := []string{}
	n := 0
	for cur.Next(ctx) && n < maxRows {
		var doc bson.M
		if err := cur.Decode(&doc); err != nil {
			continue
		}
		for k := range doc {
			if !seen[k] {
				seen[k] = true
				columns = append(columns, k)
			}
		}
		n++
	}
	sortMongoColumns(columns)
	return columns, cur.Err()
}

// sortMongoColumns puts _id first and the rest alphabetically, matching what
// the grid shows so an exported file's column order is not a surprise.
func sortMongoColumns(columns []string) {
	sort.Slice(columns, func(i, j int) bool {
		if columns[i] == "_id" {
			return true
		}
		if columns[j] == "_id" {
			return false
		}
		return columns[i] < columns[j]
	})
}

func mongoStreamCSV(ctx context.Context, cur *mongo.Cursor, columns []string, w io.Writer, maxRows int) (int, bool, error) {
	cw := csv.NewWriter(w)
	if err := cw.Write(columns); err != nil {
		return 0, false, err
	}
	rec := make([]string, len(columns))
	count, truncated := 0, false
	for cur.Next(ctx) {
		if count >= maxRows {
			truncated = true
			break
		}
		var doc bson.M
		if err := cur.Decode(&doc); err != nil {
			continue
		}
		for i, c := range columns {
			v, ok := doc[c]
			if !ok {
				rec[i] = ""
				continue
			}
			rec[i] = csvCell(normaliseBSON(v))
		}
		if err := cw.Write(rec); err != nil {
			return count, truncated, err
		}
		count++
	}
	cw.Flush()
	if err := cw.Error(); err != nil {
		return count, truncated, err
	}
	return count, truncated, cur.Err()
}

func mongoStreamJSON(ctx context.Context, cur *mongo.Cursor, w io.Writer, maxRows int) (int, bool, error) {
	if _, err := io.WriteString(w, "[\n"); err != nil {
		return 0, false, err
	}
	enc := json.NewEncoder(w)
	count, truncated := 0, false
	for cur.Next(ctx) {
		if count >= maxRows {
			truncated = true
			break
		}
		var doc bson.M
		if err := cur.Decode(&doc); err != nil {
			continue
		}
		flat := map[string]any{}
		for k, v := range doc {
			flat[k] = normaliseBSON(v)
		}
		if count > 0 {
			if _, err := io.WriteString(w, ",\n"); err != nil {
				return count, truncated, err
			}
		}
		if err := enc.Encode(flat); err != nil {
			return count, truncated, err
		}
		count++
	}
	if _, err := io.WriteString(w, "]\n"); err != nil {
		return count, truncated, err
	}
	return count, truncated, cur.Err()
}

// MongoImport loads documents into a collection from JSON or CSV.
//
// Mongo has no transaction to wrap this in on a standalone server — multi
// document transactions need a replica set — so unlike the SQL import this one
// cannot promise all-or-nothing. It therefore reports precisely what landed and
// what did not, and the UI says so rather than implying a rollback that is not
// there.
func MongoImport(
	ctx context.Context,
	client *mongo.Client,
	dbName, collection, format, data string,
	stopOnError bool,
) (*ImportResult, error) {
	docs, err := parseImportDocuments(format, data)
	if err != nil {
		return nil, err
	}
	coll := client.Database(dbName).Collection(collection)
	res := &ImportResult{Errors: []string{}, Statement: fmt.Sprintf("db.%s.insertMany(…)", collection)}
	for i, doc := range docs {
		if _, err := coll.InsertOne(ctx, doc); err != nil {
			res.Failed++
			if len(res.Errors) < maxImportErrors {
				res.Errors = append(res.Errors, fmt.Sprintf("document %d: %v", i+1, err))
			} else {
				res.Truncated = true
			}
			if stopOnError {
				return res, fmt.Errorf("document %d: %w", i+1, err)
			}
			continue
		}
		res.Inserted++
	}
	return res, nil
}

// parseImportDocuments turns either format into a list of BSON documents. CSV
// values arrive as strings and are left that way: guessing that "007" is the
// number 7, or that "true" is a boolean, silently changes data on the way in,
// and a document store has no column type to appeal to for the right answer.
func parseImportDocuments(format, data string) ([]any, error) {
	if strings.EqualFold(format, "csv") {
		cr := csv.NewReader(strings.NewReader(data))
		cr.FieldsPerRecord = -1
		header, err := cr.Read()
		if err != nil {
			return nil, fmt.Errorf("could not read the header row: %w", err)
		}
		for i := range header {
			header[i] = strings.TrimSpace(strings.TrimPrefix(header[i], "\ufeff"))
		}
		out := []any{}
		for {
			rec, err := cr.Read()
			if err != nil {
				break
			}
			doc := bson.D{}
			for i, field := range rec {
				if i < len(header) {
					doc = append(doc, bson.E{Key: header[i], Value: field})
				}
			}
			out = append(out, doc)
		}
		return out, nil
	}

	trimmed := strings.TrimSpace(data)
	if strings.HasPrefix(trimmed, "[") {
		var arr []bson.D
		if err := bson.UnmarshalExtJSON([]byte(`{"d":`+trimmed+`}`), false, &struct {
			D *[]bson.D `bson:"d"`
		}{D: &arr}); err != nil {
			return nil, fmt.Errorf("could not parse the JSON array: %w", err)
		}
		out := make([]any, 0, len(arr))
		for _, d := range arr {
			out = append(out, d)
		}
		return out, nil
	}

	// Not an array, so treat it as newline-delimited JSON — which is what
	// mongoexport produces and what people paste most often.
	out := []any{}
	for i, line := range strings.Split(trimmed, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		doc := bson.D{}
		if err := bson.UnmarshalExtJSON([]byte(line), false, &doc); err != nil {
			return nil, fmt.Errorf("line %d is not valid JSON: %w", i+1, err)
		}
		out = append(out, doc)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no documents found to import")
	}
	return out, nil
}
