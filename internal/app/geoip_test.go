package app

import (
	"bytes"
	"compress/gzip"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"testing"

	"github.com/maxmind/mmdbwriter"
	"github.com/maxmind/mmdbwriter/mmdbtype"
)

// buildTestCountryDB returns the bytes of a tiny GeoLite2-Country mmdb mapping
// 81.2.69.0/24 -> GB (81.2.69.142 is MaxMind's canonical test IP).
func buildTestCountryDB(t *testing.T) []byte {
	t.Helper()
	w, err := mmdbwriter.New(mmdbwriter.Options{
		DatabaseType: "GeoLite2-Country",
		RecordSize:   24,
	})
	if err != nil {
		t.Fatalf("mmdbwriter.New: %v", err)
	}
	_, network, err := net.ParseCIDR("81.2.69.0/24")
	if err != nil {
		t.Fatalf("ParseCIDR: %v", err)
	}
	rec := mmdbtype.Map{countryKey: mmdbtype.Map{"iso_code": mmdbtype.String("GB")}}
	if err := w.Insert(network, rec); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	var buf bytes.Buffer
	if _, err := w.WriteTo(&buf); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	return buf.Bytes()
}

func writeTemp(t *testing.T, name string, b []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadCountryDB_DisabledWhenEmpty(t *testing.T) {
	db, err := loadCountryDB("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if db != nil {
		t.Fatal("expected nil CountryDB when source is empty")
	}
}

func TestCountryDB_LookupFromFile(t *testing.T) {
	path := writeTemp(t, "test.mmdb", buildTestCountryDB(t))
	db, err := loadCountryDB(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := db.Country(netip.MustParseAddr("81.2.69.142")); got != "GB" {
		t.Errorf("known IP: got %q, want GB", got)
	}
	if got := db.Country(netip.MustParseAddr("10.0.0.1")); got != "" {
		t.Errorf("private IP: got %q, want empty", got)
	}
}

func TestCountryDB_LookupFromGzipFile(t *testing.T) {
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	if _, err := zw.Write(buildTestCountryDB(t)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	path := writeTemp(t, "test.mmdb.gz", gz.Bytes())
	db, err := loadCountryDB(path)
	if err != nil {
		t.Fatalf("load gz: %v", err)
	}
	if got := db.Country(netip.MustParseAddr("81.2.69.142")); got != "GB" {
		t.Errorf("got %q, want GB", got)
	}
}

func TestCountryDB_NilReceiver(t *testing.T) {
	var db *CountryDB
	if got := db.Country(netip.MustParseAddr("81.2.69.142")); got != "" {
		t.Errorf("nil receiver: got %q, want empty", got)
	}
}
