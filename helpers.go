// ─── Envelope & JSON helpers ──────────────────────────────────────────────────
package main

import(
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"context"
	"time"
	"fmt"
	"io"
	"strings"
)
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

func (app *application) serverError(w http.ResponseWriter, err error) {
    log.Printf("ERROR: %v", err)
    message := "the server encountered a problem and could not process your request"
    app.writeJSON(w, http.StatusInternalServerError, envelope{"error": message}, nil)
}

func (app *application) notFound(w http.ResponseWriter) {
    message := "the requested resource could not be found"
    app.writeJSON(w, http.StatusNotFound, envelope{"error": message}, nil)
}

func (app *application) badRequest(w http.ResponseWriter, message string) {
    app.writeJSON(w, http.StatusBadRequest, envelope{"error": message}, nil)
}

func (app *application) failedValidation(w http.ResponseWriter, errors map[string]string) {
    app.writeJSON(w, http.StatusUnprocessableEntity, envelope{"errors": errors}, nil)
}

// validator holds a map of validation errors.
type validator struct {
    Errors map[string]string
}

// newValidator is a helper to create a new validator instance.
func newValidator() *validator {
    return &validator{Errors: make(map[string]string)}
}

// Valid returns true if the errors map doesn't contain any entries.
func (v *validator) Valid() bool {
    return len(v.Errors) == 0
}

// Check adds an error message to the map only if a validation check is not 'ok'.
func (v *validator) Check(ok bool, key, message string) {
    if !ok {
        if _, exists := v.Errors[key]; !exists {
            v.Errors[key] = message
        }
    }
}

// between returns true if a value is between a minimum and maximum (inclusive).
func between(value, min, max int) bool {
    return value >= min && value <= max
}

// GET /health
func (app *application) health(w http.ResponseWriter, r *http.Request) {
    ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
    defer cancel()

    err := app.db.PingContext(ctx)
    dbStatus := "reachable"
    if err != nil {
        dbStatus = "unreachable: " + err.Error()
    }

    app.writeJSON(w, http.StatusOK, envelope{
        "status":    "available",
        "database":  dbStatus,
    }, nil)
}

// GET /headers
func (app *application) echoHeaders(w http.ResponseWriter, r *http.Request) {
    received := make(map[string]string)
    for name, values := range r.Header {
        received[name] = strings.Join(values, ", ")
    }
    app.writeJSON(w, http.StatusOK, envelope{"headers": received}, nil)
}