package handler

import internal "orbitjob/internal/core/app/execute/handler"

// NewWebhook creates a new webhook handler with the given HTTP client.
// If client is nil, http.DefaultClient is used.
var NewWebhook = internal.NewWebhook
