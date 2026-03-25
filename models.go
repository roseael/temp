// Students and Courses structs
package main

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
	Instructors []string `json:"instructors"`
	Enrolled    int      `json:"enrolled"`
}

