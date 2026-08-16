package main

import (
	"bytes"
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"time"

	_ "modernc.org/sqlite"
)

func doReq(method, url string, body string) string {
	fmt.Printf("\n--- %s %s ---\n", method, url)
	req, _ := http.NewRequest(method, "http://localhost:8080"+url, bytes.NewBufferString(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return ""
	}
	defer res.Body.Close()
	fmt.Printf("HTTP/1.1 %d %s\n", res.StatusCode, res.Status)
	b, _ := io.ReadAll(res.Body)
	fmt.Printf("%s\n", string(b))
	return string(b)
}

func main() {
	db, _ := sql.Open("sqlite", "nexus.db")
	defer db.Close()

	fmt.Println("=== 1. R-07 Release Auto-linking ===")
	doReq("POST", "/releases", `{"version":"v2.0","previous_version":"v1.0"}`)
	doReq("POST", "/work", `{"id":"t-r07", "type":"email", "body":"test", "idempotency_token":"tok-r07"}`)
	time.Sleep(1 * time.Second)
	doReq("GET", "/events?entity_type=work_item&entity_id=t-r07", "")

	fmt.Println("\n=== 2. R-08 Reconcile & Disagreement ===")
	doReq("POST", "/work", `{"id":"t-r08", "type":"email", "body":"test", "idempotency_token":"tok-r08"}`)
	time.Sleep(1 * time.Second)
	doReq("GET", "/cache/t-r08", "")
	doReq("POST", "/chaos/drift/t-r08", "")
	
	fmt.Println("Waiting 7 seconds for reconciler to tick twice...")
	time.Sleep(7 * time.Second)
	doReq("GET", "/events?entity_type=cache&entity_id=t-r08", "")

	fmt.Println("\n=== 3. R-09 & R-10 Honest Degradation ===")
	doReq("POST", "/chaos/dependency-down", "")
	time.Sleep(2 * time.Second)
	doReq("GET", "/cache/t-r08", "")

	fmt.Println("\n=== 4. R-11 Rate-limited Recovery (Drain) ===")
	for i := 0; i < 5; i++ {
		db.Exec("INSERT INTO work_items (id, type, body, status, idempotency_token, created_at, updated_at) VALUES (?, 't', 't', 'dead_letter', ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)", fmt.Sprintf("d-%d", i), fmt.Sprintf("tok-d-%d", i))
	}
	fmt.Println("Inserted 5 dead_letter items directly. Drainer should pick them up (20 items/s).")
	for i := 0; i < 5; i++ {
		time.Sleep(200 * time.Millisecond)
		var count int
		db.QueryRow("SELECT count(*) FROM work_items WHERE status='dead_letter'").Scan(&count)
		fmt.Printf("[%s] Dead letters remaining: %d\n", time.Now().Format("15:04:05.000"), count)
	}

	fmt.Println("\n=== 5. R-12 90-Second Diagnosis ===")
	doReq("POST", "/chaos/crash-once/w-3", "")
	doReq("POST", "/work", `{"id":"t-crash2", "type":"email", "body":"{}", "idempotency_token":"tok-crash2"}`)
	time.Sleep(2 * time.Second)
	doReq("GET", "/diagnosis", "")
}
