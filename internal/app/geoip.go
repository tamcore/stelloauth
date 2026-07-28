package app

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"os"
	"strings"
	"time"

	"github.com/oschwald/maxminddb-golang/v2"
)

// CountryDB resolves IP addresses to ISO 3166-1 alpha-2 country codes using a
// GeoLite2-Country MaxMind database held in memory.
type CountryDB struct {
	reader *maxminddb.Reader
}

// loadCountryDB loads a GeoLite2-Country database from source. An empty source
// disables the feature (returns nil, nil). An http(s):// source is fetched once;
// anything else is treated as a local file path. A gzip payload (magic 1f 8b) is
// transparently unpacked.
func loadCountryDB(source string) (*CountryDB, error) {
	if source == "" {
		return nil, nil
	}

	var raw []byte
	var err error
	if strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://") {
		raw, err = fetchURL(source)
	} else {
		raw, err = os.ReadFile(source)
	}
	if err != nil {
		return nil, fmt.Errorf("reading GeoIP DB %q: %w", source, err)
	}

	raw, err = gunzipIfNeeded(raw)
	if err != nil {
		return nil, fmt.Errorf("unpacking GeoIP DB %q: %w", source, err)
	}

	reader, err := maxminddb.OpenBytes(raw)
	if err != nil {
		return nil, fmt.Errorf("opening GeoIP DB %q: %w", source, err)
	}
	return &CountryDB{reader: reader}, nil
}

func fetchURL(url string) ([]byte, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

func gunzipIfNeeded(b []byte) ([]byte, error) {
	if len(b) < 2 || b[0] != 0x1f || b[1] != 0x8b {
		return b, nil
	}
	zr, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	defer func() { _ = zr.Close() }()
	return io.ReadAll(zr)
}

// Country returns the ISO 3166-1 alpha-2 code for ip, or "" when the DB is not
// loaded, the address is invalid/private, or no record is found. Nil-safe.
func (db *CountryDB) Country(ip netip.Addr) string {
	if db == nil || db.reader == nil || !ip.IsValid() {
		return ""
	}
	var rec struct {
		Country struct {
			ISOCode string `maxminddb:"iso_code"`
		} `maxminddb:"country"`
	}
	if err := db.reader.Lookup(ip).Decode(&rec); err != nil {
		return ""
	}
	return rec.Country.ISOCode
}
