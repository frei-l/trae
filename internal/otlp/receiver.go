// Package otlp receives OTLP/HTTP trace exports.
package otlp

import (
	"compress/gzip"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"strings"

	"github.com/frei-l/trae/internal/model"
)

// MaxBody bounds one export request after decompression.
const MaxBody = 64 << 20

// Sink takes the spans of one export request.
type Sink func(spans []model.Span) error

// Handler serves POST /v1/traces with protobuf or JSON bodies, plain or
// gzip-compressed, as the OTLP/HTTP spec describes.
func Handler(sink Sink) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/traces", func(w http.ResponseWriter, r *http.Request) {
		// Browsers exporting with fetch send a preflight first.
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		isJSON, err := contentType(r.Header.Get("Content-Type"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnsupportedMediaType)
			return
		}
		body, err := readBody(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var spans []model.Span
		if isJSON {
			spans, err = decodeJSON(body)
		} else {
			spans, err = decodeProto(body)
		}
		if err != nil {
			http.Error(w, "bad OTLP payload: "+err.Error(), http.StatusBadRequest)
			return
		}
		if len(spans) > 0 {
			if err := sink(spans); err != nil {
				log.Printf("trae: storing %d spans: %v", len(spans), err)
				http.Error(w, err.Error(), http.StatusServiceUnavailable)
				return
			}
		}
		// An empty ExportTraceServiceResponse in the request's encoding.
		if isJSON {
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, "{}")
		} else {
			w.Header().Set("Content-Type", "application/x-protobuf")
			w.WriteHeader(http.StatusOK)
		}
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			io.WriteString(w, "trae OTLP receiver: POST traces to /v1/traces\n")
			return
		}
		http.NotFound(w, r)
	})
	return mux
}

func contentType(h string) (isJSON bool, err error) {
	if h == "" {
		return false, nil
	}
	mt, _, err := mime.ParseMediaType(h)
	if err != nil {
		return false, err
	}
	switch mt {
	case "application/json":
		return true, nil
	case "application/x-protobuf", "application/protobuf", "application/octet-stream":
		return false, nil
	}
	return false, fmt.Errorf("unsupported content type %q", mt)
}

func readBody(r *http.Request) ([]byte, error) {
	var rd io.Reader = http.MaxBytesReader(nil, r.Body, MaxBody)
	switch strings.ToLower(r.Header.Get("Content-Encoding")) {
	case "", "identity":
	case "gzip":
		gz, err := gzip.NewReader(rd)
		if err != nil {
			return nil, err
		}
		defer gz.Close()
		rd = io.LimitReader(gz, MaxBody+1)
	default:
		return nil, fmt.Errorf("unsupported content encoding %q", r.Header.Get("Content-Encoding"))
	}
	b, err := io.ReadAll(rd)
	if err != nil {
		return nil, err
	}
	if len(b) > MaxBody {
		return nil, fmt.Errorf("body over %d bytes", MaxBody)
	}
	return b, nil
}
