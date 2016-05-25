package translators

import "net/http"

// Handles - a map of unique paths to their http handlers
var Handles = make(map[string]func(http.ResponseWriter, *http.Request))
