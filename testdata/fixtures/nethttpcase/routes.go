package nethttpcase

import "net/http"

func setup() {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /users/{id}", getUser)
	mux.Handle("/healthz", http.HandlerFunc(health))
}

func getUser(http.ResponseWriter, *http.Request) {}
func health(http.ResponseWriter, *http.Request)  {}
