//Students handlers
package main

import(
	"time"
	"net/http"
	"strconv"
	"context"
	"errors"
	"database/sql"
	_ "github.com/lib/pq" // ① PostgreSQL driver — the blank import registers the "postgres" driver
	//                          with database/sql. We never call lib/pq directly; the driver hooks
	//         
)

// GET /students
func (app *application) listStudents(w http.ResponseWriter, r *http.Request) {
    query := `SELECT id, name, programme, year FROM students ORDER BY id`

    ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
    defer cancel()

    rows, err := app.db.QueryContext(ctx, query)
    if err != nil {
        app.serverError(w, err)
        return
    }
    defer rows.Close()

    students := []Student{}
    for rows.Next() {
        var s Student
        err := rows.Scan(&s.ID, &s.Name, &s.Programme, &s.Year)
        if err != nil {
            app.serverError(w, err)
            return
        }
        students = append(students, s)
    }

    if err = rows.Err(); err != nil {
        app.serverError(w, err)
        return
    }

    app.writeJSON(w, http.StatusOK, envelope{"students": students}, nil)
}

// GET /students/{id}
func (app *application) getStudent(w http.ResponseWriter, r *http.Request) {
    id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
    if err != nil || id < 1 {
        app.notFound(w)
        return
    }

    query := `SELECT id, name, programme, year FROM students WHERE id = $1`

    var s Student

    ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
    defer cancel()

    err = app.db.QueryRowContext(ctx, query, id).Scan(&s.ID, &s.Name, &s.Programme, &s.Year)
    if err != nil {
        if errors.Is(err, sql.ErrNoRows) {
            app.notFound(w)
        } else {
            app.serverError(w, err)
        }
        return
    }

    app.writeJSON(w, http.StatusOK, envelope{"student": s}, nil)
}

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