package handler

import internal "orbitjob/internal/core/app/execute/handler"

// NewHTTP creates a new HTTP handler with the given HTTP client.
// If client is nil, http.DefaultClient is used.
var NewHTTP = internal.NewHTTP
