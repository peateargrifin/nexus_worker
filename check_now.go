package main
import (
	"database/sql"
	"fmt"
	"log"
	_ "modernc.org/sqlite"
)
func main() {
	db, err := sql.Open("sqlite", "nexus.db")
	if err != nil { log.Fatal(err) }
	defer db.Close()
	var count int
	db.QueryRow("SELECT restart_count FROM workers WHERE id='w-1'").Scan(&count)
	fmt.Printf(">> DB Query: Worker w-1 restart_count = %d\n", count)
}
