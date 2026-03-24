package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	_ "github.com/lib/pq" // ① PostgreSQL driver — the blank import registers the "postgres" driver
	//                          with database/sql. We never call lib/pq directly; the driver hooks
	//                          in automatically via its init() function.
)

// ─── Envelope & JSON helpers ──────────────────────────────────────────────────

type envelope map[string]any

func (app *application) writeJSON(w http.ResponseWriter, status int, data envelope, headers http.Header) error {
	js, err := json.Marshal(data)
	if err != nil {
		return err
	}
	js = append(js, '\n')
	for key, values := range headers {
		for _, v := range values {
			w.Header().Add(key, v)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, err = w.Write(js)
	return err
}

func (app *application) readJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	const maxBytes = 1_048_576
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	err := dec.Decode(dst)
	if err != nil {
		var syntaxError *json.SyntaxError
		var unmarshalTypeError *json.UnmarshalTypeError
		var invalidUnmarshalError *json.InvalidUnmarshalError
		var maxBytesError *http.MaxBytesError
		switch {
		case errors.As(err, &syntaxError):
			return fmt.Errorf("body contains badly-formed JSON (at character %d)", syntaxError.Offset)
		case errors.Is(err, io.ErrUnexpectedEOF):
			return errors.New("body contains badly-formed JSON")
		case errors.As(err, &unmarshalTypeError):
			if unmarshalTypeError.Field != "" {
				return fmt.Errorf("body contains incorrect JSON type for field %q", unmarshalTypeError.Field)
			}
			return fmt.Errorf("body contains incorrect JSON type (at character %d)", unmarshalTypeError.Offset)
		case errors.Is(err, io.EOF):
			return errors.New("body must not be empty")
		case strings.Contains(err.Error(), "unknown field"):
			fieldName := strings.TrimPrefix(err.Error(), "json: unknown field ")
			return fmt.Errorf("body contains unknown key %s", fieldName)
		case errors.As(err, &maxBytesError):
			return fmt.Errorf("body must not be larger than %d bytes", maxBytesError.Limit)
		case errors.As(err, &invalidUnmarshalError):
			panic(err)
		default:
			return err
		}
	}
	err = dec.Decode(&struct{}{})
	if !errors.Is(err, io.EOF) {
		return errors.New("body must only contain a single JSON value")
	}
	return nil
}

// ─── Models ───────────────────────────────────────────────────────────────────

type Student struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Programme string `json:"programme"`
	Year      int    `json:"year"`
}

type Course struct {
	Code        string   `json:"code"`
	Title       string   `json:"title"`
	Credits     int      `json:"credits"`
	Enrolled    int      `json:"enrolled"`
	Instructors []string `json:"instructors"`
}

// ─── Application ──────────────────────────────────────────────────────────────

// ② application holds the database connection pool.
//
// A *sql.DB is NOT a single connection — it is a managed pool that opens and
// closes connections automatically as demand fluctuates. You create it once at
// startup, store it here, and pass *application to every handler via the
// method receiver. This is the "dependency injection via struct" pattern used
// throughout this codebase.
type application struct {
	db *sql.DB
}

// ─── Handlers ─────────────────────────────────────────────────────────────────

// GET /students
// ③ READ — list all students from PostgreSQL.
//
// Query pattern:
//   - db.QueryContext returns a *sql.Rows cursor.
//   - We always defer rows.Close() immediately after checking the error so the
//     underlying connection is returned to the pool when we're done, even if
//     we return early.
//   - rows.Next() advances the cursor one row at a time.
//   - rows.Scan() maps each column into a Go variable by position.
//   - rows.Err() must be checked after the loop: the loop exits on both
//     "no more rows" and "a read error occurred". Err() tells us which.
func (app *application) listStudents(w http.ResponseWriter, r *http.Request) {

	// ③-a  The query. $1, $2 … are PostgreSQL placeholders (not ?).
	//       We pass no arguments here, but the pattern is the same when we do.
	query := `
		SELECT id, name, programme, year
		FROM students
		ORDER BY id`

	// ③-b  Build the context that governs this DB call.
	//       r.Context() is the parent — it cancels if the HTTP client disconnects.
	//       WithTimeout adds a hard 3-second deadline on top of that.
	//       The call is cancelled by whichever fires first.
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	rows, err := app.db.QueryContext(ctx, query)
	if err != nil {
		app.serverError(w, err)
		return
	}
	defer rows.Close() // ③-c  Return the connection to the pool when done.

	// ③-d  Accumulate results into a slice.
	var students []Student

	for rows.Next() {
		var s Student
		// ③-e  Scan maps columns → Go variables in SELECT order.
		err := rows.Scan(&s.ID, &s.Name, &s.Programme, &s.Year)
		if err != nil {
			app.serverError(w, err)
			return
		}
		students = append(students, s)
	}

	// ③-f  rows.Err() surfaces any error that stopped the loop early.
	if err = rows.Err(); err != nil {
		app.serverError(w, err)
		return
	}

	err = app.writeJSON(w, http.StatusOK, envelope{"students": students}, nil)
	if err != nil {
		app.serverError(w, err)
	}
}

// GET /students/{id}
// ④ READ — fetch a single student by primary key.
//
// Query pattern:
//   - db.QueryRowContext returns exactly one *sql.Row (never nil).
//   - row.Scan() returns sql.ErrNoRows when there is no matching record.
//     We translate that into a 404; all other errors are 500.
func (app *application) getStudent(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		app.notFound(w)
		return
	}

	// ④-a  QueryRowContext is used when we expect at most one row.
	//       $1 is bound to the id argument — the driver escapes it safely,
	//       preventing SQL injection.
	query := `
		SELECT id, name, programme, year
		FROM students
		WHERE id = $1`

	var s Student

	// ④-b  Build the context: r.Context() as parent (client disconnect),
	//       WithTimeout as the hard deadline (3 seconds).
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	// ④-c  Scan is called directly on the *sql.Row, not inside a loop.
	err = app.db.QueryRowContext(ctx, query, id).Scan(
		&s.ID, &s.Name, &s.Programme, &s.Year,
	)
	if err != nil {
		switch {
		case errors.Is(err, sql.ErrNoRows): // ④-d  No match → 404.
			app.notFound(w)
		default:
			app.serverError(w, err)
		}
		return
	}

	extra := http.Header{"X-Resource-Id": []string{strconv.FormatInt(id, 10)}}
	err = app.writeJSON(w, http.StatusOK, envelope{"student": s}, extra)
	if err != nil {
		app.serverError(w, err)
	}
}

// POST /students
// ⑤ WRITE — insert a new student and return its generated ID.
//
// Query pattern:
//   - We use INSERT … RETURNING id to capture the auto-generated primary key
//     in a single round-trip (no need for a separate SELECT after the insert).
//   - QueryRowContext + Scan is used instead of ExecContext because RETURNING
//     produces a result row.
func (app *application) createStudent(w http.ResponseWriter, r *http.Request) {

	var input struct {
		Name      string `json:"name"`
		Programme string `json:"programme"`
		Year      int    `json:"year"`
	}

	err := app.readJSON(w, r, &input)
	if err != nil {
		app.badRequest(w, err.Error())
		return
	}

	v := newValidator()
	v.Check(input.Name != "", "name", "must be provided")
	v.Check(len(input.Name) <= 100, "name", "must not exceed 100 characters")
	v.Check(input.Programme != "", "programme", "must be provided")
	v.Check(between(input.Year, 1, 4), "year", "must be between 1 and 4")

	if !v.Valid() {
		app.failedValidation(w, v.Errors)
		return
	}

	// ⑤-a  INSERT with RETURNING.
	//       Parameterised placeholders ($1, $2, $3) bind our Go values.
	//       PostgreSQL fills in id automatically (BIGSERIAL in the DDL).
	query := `
		INSERT INTO students (name, programme, year)
		VALUES ($1, $2, $3)
		RETURNING id`

	var newID int64

	// ⑤-b  Build the context: r.Context() as parent, 3-second hard deadline.
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	// ⑤-c  QueryRowContext executes the statement and Scan captures the
	//       returned id column. One DB round-trip does the whole job.
	err = app.db.QueryRowContext(ctx, query,
		input.Name, input.Programme, input.Year,
	).Scan(&newID)
	if err != nil {
		app.serverError(w, err)
		return
	}

	newStudent := Student{
		ID:        newID,
		Name:      input.Name,
		Programme: input.Programme,
		Year:      input.Year,
	}

	extra := http.Header{
		"Location": []string{"/students/" + strconv.FormatInt(newID, 10)},
	}
	err = app.writeJSON(w, http.StatusCreated, envelope{"student": newStudent}, extra)
	if err != nil {
		app.serverError(w, err)
	}
}

// PUT /students/{id}
// ⑥ WRITE — replace a student record.
//
// Query pattern:
//   - We use ExecContext when we don't need data back from the statement.
//   - result.RowsAffected() tells us whether the WHERE clause matched anything,
//     so we can return 404 instead of silently doing nothing.
func (app *application) updateStudent(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		app.notFound(w)
		return
	}

	var input struct {
		Name      string `json:"name"`
		Programme string `json:"programme"`
		Year      int    `json:"year"`
	}

	err = app.readJSON(w, r, &input)
	if err != nil {
		app.badRequest(w, err.Error())
		return
	}

	v := newValidator()
	v.Check(input.Name != "", "name", "must be provided")
	v.Check(len(input.Name) <= 100, "name", "must not exceed 100 characters")
	v.Check(input.Programme != "", "programme", "must be provided")
	v.Check(between(input.Year, 1, 4), "year", "must be between 1 and 4")

	if !v.Valid() {
		app.failedValidation(w, v.Errors)
		return
	}

	// ⑥-a  UPDATE. $4 is the WHERE clause argument (the id from the URL).
	query := `
		UPDATE students
		SET name = $1, programme = $2, year = $3
		WHERE id = $4`

	// ⑥-b  Build the context: r.Context() as parent, 3-second hard deadline.
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	// ⑥-c  ExecContext is used for statements that don't return rows.
	//       It returns a sql.Result with metadata about the operation.
	result, err := app.db.ExecContext(ctx, query,
		input.Name, input.Programme, input.Year, id,
	)
	if err != nil {
		app.serverError(w, err)
		return
	}

	// ⑥-d  RowsAffected reports how many rows the UPDATE touched.
	//       0 means no student had that id → 404.
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		app.serverError(w, err)
		return
	}
	if rowsAffected == 0 {
		app.notFound(w)
		return
	}

	updated := Student{ID: id, Name: input.Name, Programme: input.Programme, Year: input.Year}
	err = app.writeJSON(w, http.StatusOK, envelope{"student": updated}, nil)
	if err != nil {
		app.serverError(w, err)
	}
}

// DELETE /students/{id}
// ⑦ WRITE — remove a student.
//
// Same ExecContext + RowsAffected pattern as PUT.
// Returns 204 No Content on success (no body needed).
func (app *application) deleteStudent(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		app.notFound(w)
		return
	}

	query := `DELETE FROM students WHERE id = $1`

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	result, err := app.db.ExecContext(ctx, query, id)
	if err != nil {
		app.serverError(w, err)
		return
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		app.serverError(w, err)
		return
	}
	if rowsAffected == 0 {
		app.notFound(w)
		return
	}

	// 204 No Content — the resource is gone, there is nothing to send back.
	w.WriteHeader(http.StatusNoContent)
}

// GET /courses — still reads from memory (not yet migrated to the DB).
func (app *application) listCourses(w http.ResponseWriter, r *http.Request) {
	courses := []Course{
		{Code: "CMPS2212", Title: "GUI Programming", Credits: 3, Enrolled: 28, Instructors: []string{"Boss"}},
		{Code: "CMPS3412", Title: "Database Systems", Credits: 3, Enrolled: 22, Instructors: []string{"Dr. Ramos"}},
	}
	err := app.writeJSON(w, http.StatusOK, envelope{"courses": courses}, nil)
	if err != nil {
		app.serverError(w, err)
	}
}

// GET /health
func (app *application) health(w http.ResponseWriter, r *http.Request) {

	// ⑧  Ping the database so the health check is genuinely meaningful.
	//     Same two-layer context pattern: r.Context() as parent, hard deadline on top.
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	err := app.db.PingContext(ctx)
	dbStatus := "reachable"
	if err != nil {
		dbStatus = "unreachable: " + err.Error()
	}

	extra := http.Header{"Cache-Control": []string{"public, max-age=30"}}
	err = app.writeJSON(w, http.StatusOK, envelope{
		"status":    "available",
		"database":  dbStatus,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}, extra)
	if err != nil {
		app.serverError(w, err)
	}
}

// GET /headers
func (app *application) echoHeaders(w http.ResponseWriter, r *http.Request) {
	received := make(map[string]string, len(r.Header))
	for name, values := range r.Header {
		received[name] = strings.Join(values, ", ")
	}
	extra := http.Header{"X-Total-Headers": []string{strconv.Itoa(len(received))}}
	err := app.writeJSON(w, http.StatusOK, envelope{
		"headers_received": received,
		"count":            len(received),
	}, extra)
	if err != nil {
		app.serverError(w, err)
	}
}

// ─── Error helpers ────────────────────────────────────────────────────────────

func (app *application) serverError(w http.ResponseWriter, err error) {
	log.Printf("ERROR: %v", err)
	app.writeJSON(w, http.StatusInternalServerError, envelope{
		"error": "the server encountered a problem and could not process your request",
	}, nil)
}

func (app *application) notFound(w http.ResponseWriter) {
	app.writeJSON(w, http.StatusNotFound, envelope{
		"error": "the requested resource could not be found",
	}, nil)
}

func (app *application) badRequest(w http.ResponseWriter, msg string) {
	app.writeJSON(w, http.StatusBadRequest, envelope{"error": msg}, nil)
}

func (app *application) failedValidation(w http.ResponseWriter, errors map[string]string) {
	app.writeJSON(w, http.StatusUnprocessableEntity, envelope{"errors": errors}, nil)
}

// ─── Routes & main ────────────────────────────────────────────────────────────

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
	dsn := "postgres://postgres:postgres@localhost:5432/university?sslmode=disable"

	db, err := openDB(dsn)
	if err != nil {
		log.Fatalf("cannot open database: %v", err)
	}
	defer db.Close() // ⑨-b  Return all connections on shutdown.

	app := &application{db: db}

	mux := http.NewServeMux()

	mux.HandleFunc("GET /students",         app.listStudents)
	mux.HandleFunc("GET /students/{id}",    app.getStudent)
	mux.HandleFunc("POST /students",        app.createStudent)
	mux.HandleFunc("PUT /students/{id}",    app.updateStudent)
	mux.HandleFunc("DELETE /students/{id}", app.deleteStudent)
	mux.HandleFunc("GET /courses",          app.listCourses)
	mux.HandleFunc("GET /health",           app.health)
	mux.HandleFunc("GET /headers",          app.echoHeaders)

	log.Println("Starting server on :4000")
	log.Println()
	log.Println("  GET    /students")
	log.Println("  GET    /students/{id}")
	log.Println("  POST   /students")
	log.Println("  PUT    /students/{id}")
	log.Println("  DELETE /students/{id}")
	log.Println("  GET    /courses")
	log.Println("  GET    /health")
	log.Println("  GET    /headers")

	err = http.ListenAndServe(":4000", mux)
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