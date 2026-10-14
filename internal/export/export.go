// Package export turns LabRat data into open formats: Apache Parquet for
// numeric instrument data, PROV-O JSON-LD for lineage. Both are readable by
// pandas, polars, DuckDB, Spark, R, and every triple store without LabRat.
package export

import (
	"io"
	"time"

	"github.com/parquet-go/parquet-go"

	"github.com/Accelerate-Biotech/LabRatLDE/internal/domain"
	"github.com/Accelerate-Biotech/LabRatLDE/pkg/jsonld"
)

// ReadingRow is the on-disk Parquet schema for instrument readings.
type ReadingRow struct {
	TS           int64   `parquet:"ts,timestamp(microsecond)"`
	InstrumentID string  `parquet:"instrument_id,dict"`
	DatasetID    string  `parquet:"dataset_id,dict"`
	Channel      string  `parquet:"channel,dict"`
	Value        float64 `parquet:"value"`
	Unit         string  `parquet:"unit,dict"`
}

// RowFrom converts a timestamp into the row's microsecond representation.
func RowFrom(ts time.Time, instrumentID, datasetID, channel string, value float64, unit string) ReadingRow {
	return ReadingRow{TS: ts.UnixMicro(), InstrumentID: instrumentID, DatasetID: datasetID, Channel: channel, Value: value, Unit: unit}
}

// ParquetWriter streams rows into a zstd-compressed Parquet file. Rows are
// buffered into row groups, so memory stays bounded for any export size.
type ParquetWriter struct {
	w *parquet.GenericWriter[ReadingRow]
}

func NewParquetWriter(out io.Writer) *ParquetWriter {
	return &ParquetWriter{w: parquet.NewGenericWriter[ReadingRow](out,
		parquet.Compression(&parquet.Zstd),
		parquet.CreatedBy("labrat-lde", "dev", ""),
	)}
}

func (p *ParquetWriter) Write(rows []ReadingRow) error {
	_, err := p.w.Write(rows)
	return err
}

func (p *ParquetWriter) Close() error { return p.w.Close() }

// ProvJSONLD writes g as a W3C PROV-O JSON-LD document.
func ProvJSONLD(w io.Writer, vocab string, g *domain.Graph) error {
	nodes := g.Nodes()
	resources := make([]jsonld.Resource, 0, len(nodes))
	for _, n := range nodes {
		props := map[string]any{"kind": string(n.Kind)}
		if !n.CreatedAt.IsZero() {
			props["createdAt"] = n.CreatedAt.UTC().Format(time.RFC3339Nano)
		}
		for k, v := range n.Attrs {
			props[k] = v
		}
		resources = append(resources, jsonld.Resource{ID: iri(n.ID), Class: n.Kind.ProvClass(), Label: n.Label, Props: props})
	}
	edges := g.Edges()
	statements := make([]jsonld.Statement, 0, len(edges))
	for _, e := range edges {
		// Edges point downstream; PROV statements are made by the consequent.
		statements = append(statements, jsonld.Statement{Subject: iri(e.To), Predicate: string(e.Relation), Object: iri(e.From)})
	}
	return jsonld.Encode(w, vocab, resources, statements)
}

func iri(id string) string { return "urn:uuid:" + id }
