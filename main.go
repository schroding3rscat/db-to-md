// nolint:nilness
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"log/slog"
	"os"
	"strings"

	md "github.com/go-spectest/markdown"
)

type config struct {
	SkipTables string `json:"skip_tables"`
	SkipSchema string `json:"skip_schema"`
}

type schemaDescription struct {
	Name   string
	Tables []tableDescription
}

type tableDescription struct {
	Name        string
	Description string
	Columns     []columnDescription
}

type columnDescription struct {
	ColumnName      string
	DataType        string
	CharacterMaxLen string
	ColumnDefault   string
	IsNullable      string
	Description     string
}

func main() {
	host := flag.String("host", "", "database host")
	port := flag.String("port", "", "database port")
	db := flag.String("database", "", "database name")
	user := flag.String("user", "", "database user")
	password := flag.String("password", "", "database password")
	outfile := flag.String("out-file", "out.md", "file to output markdown")
	flag.Parse()

	if len(os.Args) <= 1 {
		flag.Usage()
		return
	}

	b, err := os.ReadFile("config.json")
	if err != nil {
		slog.Error("cannot read config", "error", err)
		return
	}

	cfg := config{}

	err = json.Unmarshal(b, &cfg)
	if err != nil {
		slog.Error("cannot parse config", "error", err)
		return
	}

	fout, err := os.Create(*outfile)
	if err != nil {
		slog.Error("cannot create output file", "error", err)
		return
	}

	pool, err := connect(context.Background(), *host, *port, *db, *user, *password)
	if err != nil {
		slog.Error("cannot connect to the database", "error", err)
		return
	}

	query := `
	SELECT DISTINCT isc.table_schema
	FROM
		information_schema.columns isc
	where table_name !~* $1
		and table_schema !~* $2
	ORDER BY isc.table_schema;`

	rowsSchema, err := pool.Query(context.Background(), query, cfg.SkipTables, cfg.SkipSchema)
	if err != nil {
		slog.Error("cannot get schema list", "error", err)
		return
	}

	schemas := make([]schemaDescription, 0)

	for rowsSchema.Next() {
		var schemaName string

		err = rowsSchema.Scan(&schemaName)
		if err != nil {
			slog.Error("cannot scan schema", "error", err)
			return
		}

		query = `
		SELECT DISTINCT
			isc.table_name,
			coalesce(obj_description(format('%s.%s', isc.table_schema, isc.table_name)::regclass::oid, 'pg_class'), '') as table_description
		FROM
			information_schema.columns isc
		WHERE table_name !~* $1
			AND table_schema = $2;`

		rowsTable, err := pool.Query(context.Background(), query, cfg.SkipTables, schemaName)
		if err != nil {
			slog.Error("cannot get tables list", "error", err)
			return
		}

		tables := make([]tableDescription, 0)

		for rowsTable.Next() {
			var tableName, tableDesc string
			err = rowsTable.Scan(&tableName, &tableDesc)
			if err != nil {
				slog.Error("cannot scan table description", "error", err)
				return
			}

			query = `
			SELECT
				column_name, data_type, coalesce(character_maximum_length::text, ''), coalesce(column_default, ''), is_nullable,
				coalesce(pg_catalog.col_description(format('%s.%s',c.table_schema,c.table_name)::regclass::oid, c.ordinal_position), '') as column_description
			FROM information_schema.columns c
			WHERE table_schema = $1 AND table_name = $2
			ORDER BY ordinal_position;`

			colrows, err := pool.Query(context.Background(), query, schemaName, tableName)
			if err != nil {
				slog.Error("cannot get columns list", "error", err)
				return
			}

			columns := make([]columnDescription, 0)

			for colrows.Next() {
				var columnName, dataType, characterMaxLen, columnDefault, isNullable, columnDesc string

				err = colrows.Scan(&columnName, &dataType, &characterMaxLen, &columnDefault, &isNullable, &columnDesc)
				if err != nil {
					slog.Error("cannot scan columns list", "error", err)
					return
				}

				columns = append(columns, columnDescription{
					ColumnName:      columnName,
					DataType:        dataType,
					CharacterMaxLen: characterMaxLen,
					ColumnDefault:   columnDefault,
					IsNullable:      isNullable,
					Description:     columnDesc,
				})
			}

			tables = append(tables, tableDescription{
				Name:        tableName,
				Description: strings.ReplaceAll(tableDesc, "\n", "<br>"),
				Columns:     columns,
			})
		}

		schemas = append(schemas, schemaDescription{
			Name:   schemaName,
			Tables: tables,
		})
	}

	buf := bufio.NewWriter(fout)

	out := md.NewMarkdown(buf).
		H1(*db).
		HorizontalRule().LF()

	for _, s := range schemas {
		out.H2(s.Name).LF()

		for _, t := range s.Tables {
			row := make([][]string, 0)

			for _, c := range t.Columns {
				row = append(row, []string{c.ColumnName, c.DataType, c.CharacterMaxLen, c.ColumnDefault, c.IsNullable,
					strings.ReplaceAll(c.Description, "\n", "<br>")})
			}

			out.H3(t.Name).LF().
				PlainText(t.Description).LF().
				CustomTable(md.TableSet{
					Header: []string{"Name", "Data type", "Character max length", "Default value", "Nullable", "Description"},
					Rows:   row,
				}, md.TableOptions{
					AutoWrapText: false,
				})
		}
	}

	err = out.Build()
	if err != nil {
		slog.Error("cannot build markdown file", "error", err)
		return
	}
}
