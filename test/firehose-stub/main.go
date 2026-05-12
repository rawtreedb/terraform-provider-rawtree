// firehose-stub is a local HTTP server that implements the Rawtree
// ?transform=firehose endpoint and a minimal query endpoint. It receives
// Kinesis Data Firehose HTTP deliveries, decodes the base64 records, stores
// them in memory keyed by table, and exposes a query endpoint so the E2E
// acceptance tests can validate ingestion counts.
//
// Usage:
//
//	go run ./test/firehose-stub -port 9876 -access-key <key>
//
// Then configure the provider with:
//
//	export RAWTREE_URL=http://localhost:9876
//
// The stub exposes:
//   - POST /v1/{org}/{project}/tables/{table}?transform=firehose  (Firehose delivery)
//   - POST /v1/{org}/{project}/query                              (SQL count queries)
//   - GET  /debug/records                                         (dump all records)
//   - GET  /healthz                                               (health check)
package main

import (
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// firehoseRequest matches the Firehose HTTP endpoint protocol v1.0.
type firehoseRequest struct {
	RequestID string           `json:"requestId"`
	Timestamp int64            `json:"timestamp"`
	Records   []firehoseRecord `json:"records"`
}

type firehoseRecord struct {
	Data string `json:"data"`
}

type firehoseResponse struct {
	RequestID    string `json:"requestId"`
	Timestamp    int64  `json:"timestamp"`
	ErrorMessage string `json:"errorMessage,omitempty"`
}

type queryRequest struct {
	SQL string `json:"sql"`
}

type queryResponse struct {
	Data []map[string]interface{} `json:"data"`
	Rows int                      `json:"rows"`
}

type store struct {
	mu      sync.RWMutex
	tables  map[string][]json.RawMessage // table -> decoded records
	deduped map[string]bool              // requestId -> already processed
}

func newStore() *store {
	return &store{
		tables:  make(map[string][]json.RawMessage),
		deduped: make(map[string]bool),
	}
}

func (s *store) ingest(table string, requestID string, records []firehoseRecord) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.deduped[requestID] {
		log.Printf("[dedup] skipping duplicate requestId=%s", requestID)
		return 0, nil
	}

	var ingested int
	for _, r := range records {
		decoded, err := base64.StdEncoding.DecodeString(r.Data)
		if err != nil {
			return ingested, fmt.Errorf("base64 decode: %w", err)
		}

		// WAF logs may be newline-delimited within a single Firehose record.
		lines := strings.Split(strings.TrimSpace(string(decoded)), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			s.tables[table] = append(s.tables[table], json.RawMessage(line))
			ingested++
		}
	}

	s.deduped[requestID] = true
	return ingested, nil
}

func (s *store) count(table string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.tables[table])
}

func (s *store) allRecords() map[string][]json.RawMessage {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string][]json.RawMessage, len(s.tables))
	for k, v := range s.tables {
		out[k] = v
	}
	return out
}

var (
	// /v1/{org}/{project}/tables/{table}
	firehosePathRe = regexp.MustCompile(`^/v1/([^/]+)/([^/]+)/tables/([^/?]+)`)
	// /v1/{org}/{project}/query
	queryPathRe = regexp.MustCompile(`^/v1/([^/]+)/([^/]+)/query$`)
	// SELECT count() as cnt FROM {table}
	countQueryRe = regexp.MustCompile(`(?i)SELECT\s+count\(\)\s+as\s+cnt\s+FROM\s+(\S+)`)
)

func main() {
	port := flag.Int("port", 9876, "listen port")
	accessKey := flag.String("access-key", "", "expected X-Amz-Firehose-Access-Key value (empty = skip validation)")
	flag.Parse()

	s := newStore()

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		fmt.Fprintln(w, "ok")
	})
	mux.HandleFunc("/debug/records", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(s.allRecords())
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Route: Firehose delivery
		if m := firehosePathRe.FindStringSubmatch(r.URL.Path); m != nil && r.URL.Query().Get("transform") == "firehose" {
			handleFirehose(w, r, s, *accessKey, m[3])
			return
		}

		// Route: Query
		if m := queryPathRe.FindStringSubmatch(r.URL.Path); m != nil {
			handleQuery(w, r, s)
			return
		}

		http.NotFound(w, r)
	})

	addr := fmt.Sprintf(":%d", *port)
	log.Printf("firehose-stub listening on %s", addr)
	if *accessKey != "" {
		log.Printf("  access-key validation: enabled")
	} else {
		log.Printf("  access-key validation: disabled (any key accepted)")
	}
	log.Fatal(http.ListenAndServe(addr, mux))
}

func handleFirehose(w http.ResponseWriter, r *http.Request, s *store, expectedKey, table string) {
	now := time.Now().UnixMilli()

	log.Println(strings.Repeat("─", 80))
	log.Printf("[firehose] >>> INCOMING REQUEST  table=%s", table)
	log.Printf("[firehose] %s %s %s", r.Method, r.URL.String(), r.Proto)

	// Dump all request headers sorted.
	log.Println("[firehose] ── Request Headers ──")
	keys := make([]string, 0, len(r.Header))
	for k := range r.Header {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		for _, v := range r.Header[k] {
			display := v
			if strings.EqualFold(k, "X-Amz-Firehose-Access-Key") && len(v) > 12 {
				display = v[:6] + "..." + v[len(v)-6:]
			}
			log.Printf("[firehose]   %s: %s", k, display)
		}
	}

	// Read raw body for logging.
	rawBody, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("[firehose] REJECTED table=%s reason=read_body err=%v", table, err)
		writeFirehoseResponse(w, r.Header.Get("X-Amz-Firehose-Request-Id"), now, "failed to read body", http.StatusBadRequest, table)
		return
	}

	log.Println("[firehose] ── Raw Request Body ──")
	prettyBody, err := indentJSON(rawBody)
	if err != nil {
		log.Printf("[firehose]   (not valid JSON) %s", string(rawBody))
	} else {
		for _, line := range strings.Split(prettyBody, "\n") {
			log.Printf("[firehose]   %s", line)
		}
	}

	// Validate access key if configured.
	if expectedKey != "" {
		got := r.Header.Get("X-Amz-Firehose-Access-Key")
		if got != expectedKey {
			log.Printf("[firehose] REJECTED table=%s reason=bad_access_key got=%q", table, got)
			writeFirehoseResponse(w, r.Header.Get("X-Amz-Firehose-Request-Id"), now, "invalid access key", http.StatusForbidden, table)
			return
		}
	}

	var req firehoseRequest
	if err := json.Unmarshal(rawBody, &req); err != nil {
		log.Printf("[firehose] REJECTED table=%s reason=bad_json err=%v", table, err)
		writeFirehoseResponse(w, "", now, "invalid JSON body", http.StatusBadRequest, table)
		return
	}

	requestID := req.RequestID
	if requestID == "" {
		requestID = r.Header.Get("X-Amz-Firehose-Request-Id")
	}

	// Decode and log each record.
	log.Printf("[firehose] ── Decoded Records (%d) ──", len(req.Records))
	for i, rec := range req.Records {
		decoded, decErr := base64.StdEncoding.DecodeString(rec.Data)
		if decErr != nil {
			log.Printf("[firehose]   record[%d]: BASE64 ERROR: %v", i, decErr)
			log.Printf("[firehose]   record[%d]: raw data = %s", i, rec.Data)
			continue
		}
		pretty, pErr := indentJSON(decoded)
		if pErr != nil {
			log.Printf("[firehose]   record[%d]: %s", i, string(decoded))
		} else {
			for j, line := range strings.Split(pretty, "\n") {
				if j == 0 {
					log.Printf("[firehose]   record[%d]: %s", i, line)
				} else {
					log.Printf("[firehose]            %s", line)
				}
			}
		}
	}

	ingested, ingestErr := s.ingest(table, requestID, req.Records)
	if ingestErr != nil {
		log.Printf("[firehose] ERROR table=%s requestId=%s err=%v", table, requestID, ingestErr)
		writeFirehoseResponse(w, requestID, now, ingestErr.Error(), http.StatusInternalServerError, table)
		return
	}

	total := s.count(table)
	log.Printf("[firehose] OK table=%s requestId=%s records_in_batch=%d ingested=%d total=%d",
		table, requestID, len(req.Records), ingested, total)
	writeFirehoseResponse(w, requestID, now, "", http.StatusOK, table)
}

func writeFirehoseResponse(w http.ResponseWriter, requestID string, ts int64, errMsg string, status int, table string) {
	resp := firehoseResponse{
		RequestID: requestID,
		Timestamp: ts,
	}
	if errMsg != "" {
		resp.ErrorMessage = errMsg
	}

	respBytes, _ := json.MarshalIndent(resp, "", "  ")

	log.Printf("[firehose] <<< RESPONSE  status=%d table=%s", status, table)
	log.Println("[firehose] ── Response Body ──")
	for _, line := range strings.Split(string(respBytes), "\n") {
		log.Printf("[firehose]   %s", line)
	}
	log.Println(strings.Repeat("─", 80))

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(respBytes)
	w.Write([]byte("\n"))
}

func indentJSON(data []byte) (string, error) {
	var v interface{}
	if err := json.Unmarshal(data, &v); err != nil {
		return "", err
	}
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func handleQuery(w http.ResponseWriter, r *http.Request, s *store) {
	var req queryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid query body", http.StatusBadRequest)
		return
	}

	m := countQueryRe.FindStringSubmatch(req.SQL)
	if m == nil {
		log.Printf("[query] unsupported query: %s", req.SQL)
		http.Error(w, "only SELECT count() as cnt FROM <table> is supported", http.StatusBadRequest)
		return
	}

	table := m[1]
	cnt := s.count(table)
	log.Printf("[query] table=%s count=%d", table, cnt)

	resp := queryResponse{
		Data: []map[string]interface{}{
			{"cnt": cnt},
		},
		Rows: 1,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
