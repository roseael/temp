// Courses handlers

package main

import (
	"context"
	"net/http"
	"time"

	"github.com/lib/pq"
	_ "github.com/lib/pq" // ① PostgreSQL driver — the blank import registers the "postgres" driver
	//                          with database/sql. We never call lib/pq directly; the driver hooks
	//
)

// GET /courses
func (app *application) listCourses(w http.ResponseWriter, r *http.Request) {
    query := `SELECT code, title, credits, instructors, enrolled FROM courses ORDER BY code`

    ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
    defer cancel()

    rows, err := app.db.QueryContext(ctx, query)
    if err != nil {
        app.serverError(w, err)
        return
    }
    defer rows.Close()

    var courses []Course
    for rows.Next() {
        var c Course
        if err := rows.Scan(&c.Code, &c.Title, &c.Credits, pq.Array(&c.Instructors), &c.Enrolled); err != nil {
            app.serverError(w, err)
            return
        }
        courses = append(courses, c)
    }

    if err = rows.Err(); err != nil {
        app.serverError(w, err)
        return
    }

    app.writeJSON(w, http.StatusOK, envelope{"courses": courses}, nil)
}

// POST /courses
func (app *application) createCourse(w http.ResponseWriter, r *http.Request) {
    var input struct {
        Code    string `json:"code"`
        Title   string `json:"title"`
        Credits int    `json:"credits"`
        Instructors []string `json:"instructors"`
    }

    if err := app.readJSON(w, r, &input); err != nil {
        app.badRequest(w, err.Error())
        return
    }

    query := `
        INSERT INTO courses (code, title, credits, instructors)
        VALUES ($1, $2, $3, $4)
        RETURNING code, enrolled`

    ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
    defer cancel()

    var course Course
    course.Code = input.Code
    course.Title = input.Title
    course.Credits = input.Credits
    course.Instructors = input.Instructors

    err := app.db.QueryRowContext(ctx, query, input.Code, input.Title, input.Credits, pq.Array(input.Instructors)).
        Scan(&course.Code, &course.Enrolled)
    
    if err != nil {
        app.serverError(w, err)
        return
    }

    app.writeJSON(w, http.StatusCreated, envelope{"course": course}, nil)
}
// PUT /courses
func (app *application) updateCourse(w http.ResponseWriter, r *http.Request) {
    code := r.PathValue("code")

    var input struct {
        Title   string `json:"title"`
        Credits int    `json:"credits"`
    }

    if err := app.readJSON(w, r, &input); err != nil {
        app.badRequest(w, err.Error())
        return
    }

    query := `
        UPDATE courses 
        SET title = $1, credits = $2 
        WHERE code = $3`

    ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
    defer cancel()

    result, err := app.db.ExecContext(ctx, query, input.Title, input.Credits, code)
    if err != nil {
        app.serverError(w, err)
        return
    }

    rowsAffected, _ := result.RowsAffected()
    if rowsAffected == 0 {
        app.notFound(w)
        return
    }

    // Return the updated object
    app.writeJSON(w, http.StatusOK, envelope{"course": input}, nil)
}
// DELETE /courses
func (app *application) deleteCourse(w http.ResponseWriter, r *http.Request) {
    code := r.PathValue("code")

    query := `DELETE FROM courses WHERE code = $1`

    ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
    defer cancel()

    result, err := app.db.ExecContext(ctx, query, code)
    if err != nil {
        app.serverError(w, err)
        return
    }

    rowsAffected, _ := result.RowsAffected()
    if rowsAffected == 0 {
        app.notFound(w)
        return
    }

    w.WriteHeader(http.StatusNoContent)
}