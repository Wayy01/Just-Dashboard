package dbx

import (
	"strings"
	"testing"
)

func ormFixture() ([]Table, map[string]*TableDetail) {
	tables := []Table{
		{Schema: "public", Name: "users", Type: "table"},
		{Schema: "public", Name: "posts", Type: "table"},
		{Schema: "public", Name: "user_view", Type: "view"},
	}
	details := map[string]*TableDetail{
		"users": {
			Schema: "public", Name: "users",
			Columns: []Column{
				{Name: "id", Type: "integer", Nullable: false},
				{Name: "email", Type: "varchar(255)", Nullable: false},
				{Name: "name", Type: "text", Nullable: true},
			},
			PrimaryKey: []string{"id"},
			Indexes: []Index{
				{Name: "users_pkey", Columns: []string{"id"}, Unique: true, Primary: true},
				{Name: "users_email_key", Columns: []string{"email"}, Unique: true},
			},
		},
		"posts": {
			Schema: "public", Name: "posts",
			Columns: []Column{
				{Name: "id", Type: "bigint", Nullable: false},
				{Name: "title", Type: "text", Nullable: false},
				{Name: "author_id", Type: "integer", Nullable: false},
			},
			PrimaryKey: []string{"id"},
			ForeignKeys: []ForeignKey{
				{Name: "posts_author_fk", Columns: []string{"author_id"},
					RefSchema: "public", RefTable: "users", RefColumns: []string{"id"}},
			},
		},
	}
	return tables, details
}

func TestGeneratePrismaSchema(t *testing.T) {
	tables, details := ormFixture()
	out := GeneratePrismaSchema(DriverPostgres, tables, details)

	must := []string{
		`provider = "postgresql"`,
		"model users {",
		"model posts {",
		"id Int @id",           // single-column PK inline
		"id BigInt @id",        // bigint maps to BigInt
		"email String @unique", // unique index -> @unique
		"name String?",         // nullable -> optional
		`@relation("posts_author_id", fields: [author_id], references: [id])`,
		"posts[]", // back-relation on users
	}
	for _, m := range must {
		if !strings.Contains(out, m) {
			t.Errorf("prisma schema missing %q\n---\n%s", m, out)
		}
	}
	// A view must not become a model.
	if strings.Contains(out, "model user_view") {
		t.Errorf("prisma schema included a view as a model:\n%s", out)
	}
}

func TestGeneratePrismaProviders(t *testing.T) {
	tables, details := ormFixture()
	if !strings.Contains(GeneratePrismaSchema(DriverMySQL, tables, details), `provider = "mysql"`) {
		t.Error("mysql provider not rendered")
	}
	if !strings.Contains(GeneratePrismaSchema(DriverSQLite, tables, details), `provider = "sqlite"`) {
		t.Error("sqlite provider not rendered")
	}
}

func TestGenerateDrizzleSchema(t *testing.T) {
	tables, details := ormFixture()
	out := GenerateDrizzleSchema(DriverPostgres, tables, details)
	must := []string{
		`from "drizzle-orm/pg-core"`,
		`pgTable("users"`,
		`pgTable("posts"`,
		".primaryKey()",
		".notNull()",
		".unique()",
	}
	for _, m := range must {
		if !strings.Contains(out, m) {
			t.Errorf("drizzle schema missing %q\n---\n%s", m, out)
		}
	}
	if strings.Contains(out, "user_view") {
		t.Errorf("drizzle schema included a view:\n%s", out)
	}
}

func TestGenerateORMRejectsMongo(t *testing.T) {
	if _, err := GenerateORM(ORMPrisma, DriverMongo, nil, nil); err == nil {
		t.Error("expected ORM generation to reject MongoDB")
	}
}

func TestBaseType(t *testing.T) {
	cases := map[string]string{
		"varchar(255)":             "varchar",
		"NUMERIC(10,2)":            "numeric",
		"integer":                  "integer",
		"int unsigned":             "int",
		"timestamp with time zone": "timestamp with time zone",
		"double precision":         "double precision",
		"text[]":                   "text",
	}
	for in, want := range cases {
		if got := baseType(in); got != want {
			t.Errorf("baseType(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPrismaType(t *testing.T) {
	cases := map[string]string{
		"integer":      "Int",
		"bigint":       "BigInt",
		"varchar(255)": "String",
		"boolean":      "Boolean",
		"jsonb":        "Json",
		"timestamptz":  "DateTime",
		"bytea":        "Bytes",
		"numeric(8,2)": "Decimal",
	}
	for in, want := range cases {
		if got := prismaType(in); got != want {
			t.Errorf("prismaType(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSanitizeIdent(t *testing.T) {
	cases := map[string]string{
		"users":     "users",
		"user$name": "user_name",
		"2fa":       "_2fa",
		"weird-col": "weird_col",
	}
	for in, want := range cases {
		if got := sanitizeIdent(in); got != want {
			t.Errorf("sanitizeIdent(%q) = %q, want %q", in, got, want)
		}
	}
}
