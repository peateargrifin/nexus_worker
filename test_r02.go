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

	fmt.Println("=== 6. R-02 Dead-Letter Visibility (CORE) ===")
	// Disable drainer so it doesn't instantly sweep the dead-letter
	doReq("POST", "/chaos/toggle-drainer", "")

	// Post a work item (we will force max_attempts = 1 implicitly by failing it enough times, but wait:
	// item.MaxAttempts is 5. We need to fail it 5 times!)
	doReq("POST", "/work", `{"id":"t-r02", "type":"email", "body":"test", "idempotency_token":"tok-r02"}`)
	
	// Force the workers to fail the work 5 times
	doReq("POST", "/chaos/fail-work/w-1", "")
	doReq("POST", "/chaos/fail-work/w-2", "")
	doReq("POST", "/chaos/fail-work/w-3", "")

	// Since they back off exponentially, 5 fails might take a long time to schedule!
	// 1st fail: backoff 2^1 = 2s
	// 2nd fail: backoff 2^2 = 4s
	// 3rd fail: 8s
	// This takes too long for a fast test. Let's send a custom max_attempts = 1!
	// In the real code, MaxAttempts is unmarshaled from JSON.
	doReq("POST", "/work", `{"id":"t-r02-fast", "type":"email", "body":"test", "idempotency_token":"tok-r02-fast", "max_attempts": 1}`)
	
	time.Sleep(3 * time.Second)

	// Check DB
	var status, dlReason string
	var attemptCount, maxAttempts int
	err := db.QueryRow("SELECT status, attempt_count, max_attempts, IFNULL(dead_letter_reason, '') FROM work_items WHERE id = 't-r02-fast'").Scan(&status, &attemptCount, &maxAttempts, &dlReason)
	fmt.Printf(">> DB Query: ID=t-r02-fast, Status=%s, Attempts=%d/%d, DL_Reason='%s' (err: %v)\n", status, attemptCount, maxAttempts, dlReason, err)

	// Check Events (dead_lettered)
	doReq("GET", "/events?entity_type=work_item&entity_id=t-r02-fast", "")
}
