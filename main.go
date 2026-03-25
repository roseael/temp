package main
import(
	"database/sql"
	"context"
	"log"
	"net/http"
	"time"
	_ "github.com/lib/pq" // ① PostgreSQL driver — the blank import registers the "postgres" driver
	//                          with database/sql. We never call lib/pq directly; the driver hooks
	//   
)

type application struct {
    db *sql.DB
}

func main() {

	// ⑨  Open the connection pool.
	//
	//     sql.Open does NOT open a real connection — it only validates the DSN
	//     and registers the pool. The first actual connection happens lazily
	//     when a query is executed. db.Ping() (or PingContext) forces that
	//     first connection immediately so we fail fast at startup rather than
	//     discovering a bad DSN on the first request.
	//
	//     DSN format for lib/pq:
	//       postgres://<user>:<password>@<host>:<port>/<dbname>?sslmode=disable
	//
	//     For a local development database with no password and SSL disabled:
	dsn := "postgres://uni:uni1@localhost:5432/university?sslmode=disable"

	db, err := openDB(dsn)
	if err != nil {
		log.Fatalf("cannot open database: %v", err)
	}
	defer db.Close() // ⑨-b  Return all connections on shutdown.

	app := &application{db: db}

	log.Println("Starting server on :4000")
    err = http.ListenAndServe(":4000", app.routes())
    log.Fatal(err)
}
// openDB encapsulates the boilerplate for building a *sql.DB and verifying it.
//
// ⑩  Connection pool configuration.
//
//	SetMaxOpenConns   — cap the total number of open connections.
//	                    Prevents overwhelming PostgreSQL when traffic spikes.
//	                    A common starting point is 25; tune for your hardware.
//
//	SetMaxIdleConns   — how many connections are kept open but unused.
//	                    Should be <= MaxOpenConns. Keeping some idle avoids the
//	                    latency of opening a new connection on every request.
//
//	SetConnMaxIdleTime — discard a connection that has been idle for longer
//	                     than this duration. Prefer this over SetConnMaxLifetime:
//	                     it reclaims connections that are genuinely unused rather
//	                     than ones that are merely old.
func openDB(dsn string) (*sql.DB, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(10)
	db.SetConnMaxIdleTime(15 * time.Minute)

	// Force the pool to open its first real connection, with a hard deadline.
	// Using context.WithTimeout here means the startup fails fast if PostgreSQL
	// is unreachable, rather than blocking indefinitely.
	// This is the same context discipline we apply to every query in the handlers.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = db.PingContext(ctx)
	if err != nil {
		db.Close()
		return nil, err
	}

	log.Println("Database connection pool established")
	return db, nil
}
