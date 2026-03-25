package main

import (
	"net/http"
)

func (app *application) routes() *http.ServeMux {
    mux := http.NewServeMux()
    mux.HandleFunc("GET /students", app.listStudents)
	mux.HandleFunc("GET /students/{id}",    app.getStudent)
	mux.HandleFunc("POST /students",        app.createStudent)
	mux.HandleFunc("PUT /students/{id}",    app.updateStudent)
	mux.HandleFunc("DELETE /students/{id}", app.deleteStudent)

	mux.HandleFunc("GET /courses",          app.listCourses)
	mux.HandleFunc("POST /courses",        app.createCourse)
    mux.HandleFunc("DELETE /courses/{code}", app.deleteCourse)
	mux.HandleFunc("PUT /courses/{code}",   app.updateCourse)

	mux.HandleFunc("GET /health",           app.health)
	mux.HandleFunc("GET /headers",          app.echoHeaders)

    return mux
}
