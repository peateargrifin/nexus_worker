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

	fmt.Println("=== Verifying R-01 (Worker Death Recovery) ===")
	
	// Ensure we have a fresh item
	doReq("POST", "/work", `{"id":"t-r01-death", "type":"email", "body":"survive", "idempotency_token":"tok-r01-death"}`)

	// Give the dispatcher time to assign it to a worker (takes up to 2 seconds due to ticker)
	var status, assigned string
	for i := 0; i < 50; i++ {
		db.QueryRow("SELECT status, IFNULL(assigned_worker, '') FROM work_items WHERE id = 't-r01-death'").Scan(&status, &assigned)
		if status == "assigned" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	fmt.Printf(">> DB Query 1: ID=t-r01-death, Status=%s, AssignedWorker='%s'\n", status, assigned)

	if status != "assigned" {
		fmt.Println("Error: Item was not assigned. Cannot test death recovery.")
		return
	}

	// Kill the worker that is processing it
	doReq("POST", "/chaos/kill-worker/" + assigned, "")

	// Give the platform a second to run the recovery logic
	time.Sleep(1 * time.Second)

	var statusAfter string
	var assignedAfter string
	var attemptCount int
	db.QueryRow("SELECT status, attempt_count, IFNULL(assigned_worker, '') FROM work_items WHERE id = 't-r01-death'").Scan(&statusAfter, &attemptCount, &assignedAfter)
	fmt.Printf(">> DB Query 2: ID=t-r01-death, Status=%s, AttemptCount=%d, AssignedWorker='%s'\n", statusAfter, attemptCount, assignedAfter)
}
