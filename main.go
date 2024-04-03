package main

import (
	"net/http"

	"github.com/gorilla/mux"
)

func main() {
	r := mux.NewRouter()

	fs := http.FileServer(http.Dir("static"))
	r.PathPrefix("/").Handler(http.StripPrefix("/", fs))

	http.ListenAndServe(":80", r)
}
